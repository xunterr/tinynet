package internal

import (
	"net"
	"sync"
)

type transport struct {
	net.Conn
	failed func()
}

func (t *transport) Read(p []byte) (n int, err error) {
	n, err = t.Conn.Read(p)
	t.checkErr(err)
	return
}

func (t *transport) Write(p []byte) (n int, err error) {
	n, err = t.Conn.Write(p)
	t.checkErr(err)
	return
}

func (t *transport) checkErr(err error) {
	if err != nil {
		t.failed()
	}
}

type Conn struct {
	node *Node

	connId   []byte
	remoteId []byte

	conn net.Conn
	mux  Mux

	ready chan struct{}
	once  sync.Once
}

func newConn(connId []byte, mux Mux, t net.Conn) *Conn {
	conn := &Conn{
		connId: connId,
		mux:    mux,
		ready:  make(chan struct{}),
	}
	conn.conn = t
	return conn
}

func (c *Conn) IsActive() bool {
	if c.mux != nil {
		return !c.mux.IsClosed()
	}
	return false
}

func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		c.node.deleteConn(c)
		c.mux.Close()
		err = c.conn.Close()
	})
	return err
}

func (c *Conn) TryClose() error {
	if !c.IsActive() {
		return nil
	}
	return c.Close()
}

func (c *Conn) OpenStream() (*Stream, error) {
	mStream, err := c.mux.Open()
	if err != nil {
		c.Close()
		return nil, err
	}

	s := newStream(mStream)
	return s, nil
}

func (c *Conn) AcceptStream() (*Stream, error) {
	mStream, err := c.mux.Accept()
	if err != nil {
		c.Close()
		return nil, err
	}

	s := newStream(mStream)
	return s, nil
}
