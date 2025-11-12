package transports

import (
	"errors"
	"io"
	"net"
	"sync"
)

type Mux interface {
	Open() (net.Conn, error)
	Accept() (net.Conn, error)
}

type Router interface {
	Route(Message)
}

type Message struct {
	From    string
	To      string
	Payload *Payload
}

type Payload struct {
	NodeId        string
	MessageId     string
	CorrelationId string
	Version       int
	Body          []byte
}

type MuxFunc func(net.Conn) (Mux, error)

type asymMux struct {
	client MuxFunc
	server MuxFunc
}

// set up a muxer
func WithMux(client MuxFunc, server MuxFunc) {}

// sets up a symmetrical muxer (client and server muxers are the same)
func SymMux(m MuxFunc) (MuxFunc, MuxFunc) {
	return m, m
}

type Transports struct {
	router Router

	nodeId string

	connMu      sync.RWMutex
	connections map[string]Mux

	mux asymMux
}

func NewTransports() *Transports {
	return &Transports{}
}

func (t *Transports) dial(addr string) (io.ReadWriteCloser, error) {
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

func (t *Transports) Send(addr string, data []byte) error { return nil }

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
		_, err := mux.Accept()
		if err != nil {
			if _, ok := err.(net.Error); ok {
				return err
			}
			continue
		}
		// go t.handle(c)
	}
}

var ErrPartialRead error = errors.New("Partially read message")

type Stream struct { // <-!!!
	net.Conn
	n int
}

// Reads at most one message from Stream.
// If p isn't large enough, reads len(p) bytes, and
// subsequent reads will read the rest of the message
func (s *Stream) Read(p []byte) (n int, err error) {
	if s.n == 0 {
		header, err := decodeMessageHeader(s.Conn)
		if err != nil {
			return 0, err
		}
		s.n = int(header.Length)
	}

	right := min(len(p), s.n)
	readN, err := io.ReadFull(s.Conn, p[:right])
	s.n -= readN
	if err != nil {
		s.n = 0
		if err != io.ErrUnexpectedEOF {
			return 0, err
		}
	}
	return readN, nil
}

// Reads at most one message from Stream.
// ErrPartialRead if there is a partially read message
func (s *Stream) ReadFull() ([]byte, error) {
	if s.n != 0 {
		return []byte{}, ErrPartialRead
	}

	h, err := decodeMessageHeader(s.Conn)
	if err != nil {
		return []byte{}, err
	}

	bytes := make([]byte, h.Length)
	_, err = io.ReadFull(s.Conn, bytes)
	if err != nil && err != io.ErrUnexpectedEOF {
		return []byte{}, err
	}
	return bytes, nil
}

// Writes message to Stream
func (s *Stream) Write(p []byte) (n int, err error) {
	header := messageHeader{
		Version: 0,
		Type:    0,
		Length:  uint32(len(p)),
	}
	err = encodeMessageHeader(s.Conn, header)
	if err != nil {
		return
	}

	return s.Conn.Write(p)
}
