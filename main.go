package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/xunterr/tinynet/internal"
)

func main() {
	t := internal.NewTransports(internal.WithMux(
		func(c net.Conn) (internal.Mux, error) { return yamux.Client(c, nil) },
		func(c net.Conn) (internal.Mux, error) { return yamux.Server(c, nil) },
	))

	t2 := internal.NewTransports(internal.WithMux(
		func(c net.Conn) (internal.Mux, error) { return yamux.Client(c, nil) },
		func(c net.Conn) (internal.Mux, error) { return yamux.Server(c, nil) },
	))

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		err := t.Listen("127.0.0.1:6969")
		if err != nil {
			panic(err.Error())
		}
		wg.Done()
	}()

	go func() {
		err := t2.Listen("127.0.0.1:6767")
		if err != nil {
			panic(err.Error())
		}
		wg.Done()
	}()

	t.SetHandleFunc(func(s *internal.Stream, h internal.Headers) {
		fmt.Printf("t#1 ---- %s \n", s.Conn.RemoteAddr().String())
	})
	t2.SetHandleFunc(func(s *internal.Stream, h internal.Headers) {
		fmt.Printf("t#2 ---- %s \n", s.Conn.RemoteAddr().String())
	})

	time.Sleep(2 * time.Second)

	go func() {
		c, err := t.Dial("127.0.0.1:6767")
		if err != nil {
			log.Printf("67 -- %s", err.Error())
			return
		}
		c.Write([]byte("hi"))
	}()

	go func() {
		c, err := t2.Dial("127.0.0.1:6969")
		if err != nil {
			log.Printf("69 -- %s", err.Error())
			return
		}
		c.Write([]byte("hi2"))
	}()

	wg.Wait()
}
