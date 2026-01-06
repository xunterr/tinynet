package main

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/xunterr/tinynet/internal"
)

func main() {
	t := internal.NewNode(internal.WithMux(
		func(c net.Conn) (internal.Mux, error) { return yamux.Client(c, nil) },
		func(c net.Conn) (internal.Mux, error) { return yamux.Server(c, nil) },
	))

	t2 := internal.NewNode(internal.WithMux(
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

	t.SetHandleFunc(handle)
	t2.SetHandleFunc(handle)

	time.Sleep(2 * time.Second)

	go func() {
		c, err := t.Dial("127.0.0.1:6767")
		if err != nil {
			log.Printf("67 -- %s", err.Error())
			return
		}
		s, err := c.OpenStream()
		if err != nil {
			panic(err.Error())
		}

		s.Write([]byte("hi"))
	}()

	go func() {
		_, err := t2.PickOrDial("127.0.0.1:6969")
		if err != nil {
			log.Printf("69 -- %s", err.Error())
			return
		}
	}()

	wg.Wait()
}

func handle(c *internal.Conn) {
	for {
		s, err := c.AcceptStream()
		if err != nil {
			panic(err.Error())
		}

		for {
			b, err := s.ReadFull()
			if err != nil {
				panic(err)
			}
			log.Println(string(b))
		}
	}
}
