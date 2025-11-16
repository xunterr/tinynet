package transports

import (
	"net"
	"sync"
)

type Mux interface {
	Open() (net.Conn, error)
	Accept() (net.Conn, error)
}

type MuxFunc func(net.Conn) (Mux, error)

type asymMux struct {
	client MuxFunc
	server MuxFunc
}

// set up a muxer
func WithMux(client MuxFunc, server MuxFunc) Option {
	return func(t *Transports) {
		t.mux = asymMux{
			client: client,
			server: server,
		}
	}
}

// sets up a symmetrical muxer (client and server muxers are the same)
func SymMux(m MuxFunc) (MuxFunc, MuxFunc) {
	return m, m
}

type Option func(*Transports)

type Transports struct {
	handle func(*Stream)

	connMu      sync.RWMutex
	connections map[string]Mux

	mux asymMux
}

func NewTransports(opts ...Option) *Transports {
	t := &Transports{
		connections: make(map[string]Mux),
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

func (t *Transports) SetMux(client MuxFunc, server MuxFunc) {
	t.mux = asymMux{
		client: client,
		server: server,
	}
}

func (t *Transports) SetHandleFunc(h func(*Stream)) {
	t.handle = h
}

func (t *Transports) dial(addr string) (net.Conn, error) {
	t.connMu.RLock()
	mux, ok := t.connections[addr]
	t.connMu.RUnlock()
	if !ok {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return nil, err
		}

		mux, err = t.mux.client(c)
		if err != nil {
			return nil, err
		}

		t.connMu.Lock()
		t.connections[addr] = mux
		t.connMu.Unlock()
	}
	return mux.Open()
}

func (t *Transports) Dial(addr string) (*Stream, error) {
	c, err := t.dial(addr)
	if err != nil {
		return nil, err
	}
	return newStream(c), nil
}

func (t *Transports) Listen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil
	}

	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}

		mux, err := t.mux.server(c)
		if err != nil {
			continue
		}

		t.connMu.Lock()
		t.connections[addr] = mux
		t.connMu.Unlock()

		go t.handleSession(mux)
	}
}

func (t *Transports) handleSession(mux Mux) error {
	for {
		c, err := mux.Accept()
		if err != nil {
			if _, ok := err.(net.Error); ok {
				return err
			}
			continue
		}

		s := newStream(c)
		go t.handle(s)
	}
}
