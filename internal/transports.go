package internal

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"log"
	"net"
	"sync"

	"github.com/google/uuid"
)

const (
	ConnAccepted byte = iota
	ConnRejected
)

var ErrHandshakeRefused = errors.New("Remote node refused a handshake.")
var ErrConcurrent = errors.New("Concurrent dials.")
var ErrConnLimit = errors.New("Per-node connection limit reached.")

// Used on Stream initialization for sharing stream-scope headers.
type Handshake interface {
	Accept(*Stream) ([]Header, error)
	Propose(*Stream, []Header) error
}

type Mux interface {
	Open() (net.Conn, error)
	Accept() (net.Conn, error)
	NumStreams() int
	Close() error
}

type MuxFunc func(net.Conn) (Mux, error)

type asymMux struct {
	client MuxFunc
	server MuxFunc
}

type Option func(*Transports)

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

func WithHandshake(h Handshake) Option {
	return func(t *Transports) { t.handshake = h }
}

type conn struct {
	connId []byte

	conn net.Conn
	mux  Mux

	ready chan struct{}

	onClose func()
}

func (c *conn) isActive() bool {
	if c.mux != nil {
		return c.mux.NumStreams() != 0
	}
	return false
}

func (c *conn) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	c.mux.Close()
	return c.conn.Close()
}

type dialAttempt struct {
	active bool
	ch     chan struct{}
	c      *conn
	err    error
}

func (d *dialAttempt) notify(c *conn, err error) {
	d.c = c
	d.err = err
	close(d.ch)
	d.active = false
}

type connPool struct {
	sync.Mutex
	p     []*conn
	limit int
	da    dialAttempt
}

func (p *connPool) sweep() {
	for _, c := range p.p {
		if !c.isActive() {
			c.Close()
		}
	}
}

func (p *connPool) pick() (*conn, bool) {
	for _, c := range p.p {
		if c.isActive() {
			return c, true
		} else {
			p.delete(c)
		}
	}
	return nil, false
}

func (p *connPool) canAppend() bool {
	return len(p.p) < p.limit
}

func (p *connPool) append(c *conn) bool {
	if !p.canAppend() {
		return false
	}
	p.p = append(p.p, c)
	return true
}

func (p *connPool) delete(c *conn) bool {
	for i, v := range p.p {
		if bytes.Compare(c.connId, v.connId) == 0 {
			last := len(p.p) - 1
			p.p[last], p.p[i] = p.p[i], p.p[last]
			p.p = p.p[:last]
			return true
		}
	}
	return false
}

type Transports struct {
	handle func(*Stream, Headers)

	handshake Handshake

	nodeId []byte

	connMu         sync.RWMutex
	connections    map[string]*connPool
	maxConnPerNode int

	mux asymMux

	once sync.Once
}

func NewTransports(opts ...Option) *Transports {
	t := &Transports{
		connections:    make(map[string]*connPool),
		handshake:      &DefaultHandshake{},
		maxConnPerNode: 1,
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

func makeNodeId(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:16]
}

func (t *Transports) SetMux(client MuxFunc, server MuxFunc) {
	t.mux = asymMux{
		client: client,
		server: server,
	}
}

func (t *Transports) SetHandleFunc(h func(*Stream, Headers)) {
	t.handle = h
}

func (t *Transports) getOrMakePool(key string) *connPool {
	t.connMu.Lock()
	p, ok := t.connections[key]
	if !ok {
		p = &connPool{
			p:     make([]*conn, 0, t.maxConnPerNode),
			limit: t.maxConnPerNode,
		}
		t.connections[key] = p
	}
	t.connMu.Unlock()
	return p
}

func (t *Transports) dial(addr string) (*conn, error) {
	p := t.getOrMakePool(string(makeNodeId(addr)))
	p.Lock()
	if c, ok := p.pick(); ok {
		p.Unlock()
		<-c.ready
		return c, nil
	}

	var c *conn
	var err error
	if !p.da.active {
		if !p.canAppend() {
			p.Unlock()
			return nil, ErrConnLimit
		}
		p.da.ch = make(chan struct{})
		p.da.active = true
		p.Unlock()
		c, err = t.createConn(addr) // todo: timeouts
		c.onClose = func() { deleteConn(p, c) }
		p.Lock()
		if err == nil {
			p.append(c)
			close(c.ready)
		}
		p.da.notify(c, err)
		p.Unlock()
		go t.handleConn(c)
	} else {
		p.Unlock()
		<-p.da.ch
		c, err = p.da.c, p.da.err
	}

	return c, err
}

func (t *Transports) createConn(addr string) (*conn, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := t.setupOutConn(c)
	if err != nil {
		c.Close()
		return nil, err
	}

	return conn, nil
}

func (t *Transports) setupOutConn(c net.Conn) (*conn, error) {
	_, err := t.shareId(c)
	if err != nil {
		return nil, err
	}

	connId, err := t.recvHandshake(c)
	if err != nil {
		return nil, err
	}

	// todo: select roles in case of TCP simultaneous open

	mux, err := t.mux.client(c)
	if err != nil {
		return nil, err
	}

	return &conn{
		conn:   c,
		connId: connId,
		mux:    mux,
		ready:  make(chan struct{}),
	}, nil
}

func (t *Transports) recvHandshake(c net.Conn) ([]byte, error) {
	buf := make([]byte, 1)
	_, err := c.Read(buf)
	if err != nil {
		return nil, err
	}

	if buf[0] == ConnRejected {
		return nil, ErrHandshakeRefused
	}

	id := make([]byte, 16)
	_, err = io.ReadFull(c, id)
	if err != nil {
		return nil, err
	}
	return id, nil
}

func (t *Transports) Dial(addr string, headers ...Header) (*Stream, error) {
	c, err := t.dial(addr)
	if err != nil {
		return nil, err
	}

	mStream, err := c.mux.Open()
	if err != nil {
		return nil, err
	}

	s := newStream(mStream)
	err = t.handshake.Propose(s, headers)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (t *Transports) Listen(addr string) error {
	var err error
	t.once.Do(func() {
		t.nodeId = makeNodeId(addr)
		err = t.listen(addr)
	})
	return err
}

func (t *Transports) listen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	for {
		c, err := l.Accept()
		if err != nil {
			log.Println(err.Error())
			continue
		}

		go func() {
			conn, err := t.tryAccept(c)
			if err != nil {
				log.Println(err.Error())
				return
			}
			t.handleConn(conn)
		}()
	}
}

func (t *Transports) tryAccept(c net.Conn) (*conn, error) {
	remoteId, err := t.shareId(c)
	if err != nil {
		return nil, err
	}

	p := t.getOrMakePool(string(remoteId))
	p.Lock()
	won := p.da.active && bytes.Compare(remoteId, t.nodeId) <= 0
	if won { // if there are concurrent requests AND we won
		p.Unlock()
		sendReject(c)
		c.Close()
		return nil, ErrConcurrent
	}
	if !p.canAppend() {
		p.Unlock()
		sendReject(c)
		c.Close()
		return nil, ErrConnLimit
	}

	connId := uuid.New()
	connIdBytes, _ := connId.MarshalBinary()
	conn := &conn{
		conn:   c,
		connId: connIdBytes,
		ready:  make(chan struct{}),
	}
	conn.onClose = func() { deleteConn(p, conn) }
	p.append(conn)
	p.Unlock()

	if err = t.accept(conn); err != nil {
		conn.Close()
		p.Lock()
		p.delete(conn)
		p.Unlock()
		return nil, err
	}

	close(conn.ready)
	return conn, nil
}

func deleteConn(p *connPool, c *conn) {
	p.Lock()
	p.delete(c)
	p.Unlock()
}

func (t *Transports) shareId(c net.Conn) ([]byte, error) {
	_, err := c.Write(t.nodeId)
	if err != nil {
		return nil, err
	}

	remoteId := make([]byte, 16)
	_, err = io.ReadFull(c, remoteId)
	return remoteId, err
}

// 		1	   |  16
// ----------------
// Status | ConnId

func sendReject(c net.Conn) error {
	_, err := c.Write([]byte{ConnRejected})
	return err
}

func sendAccept(c net.Conn, connId []byte) error {
	_, err := c.Write([]byte{ConnAccepted})
	if err != nil {
		return err
	}
	_, err = c.Write(connId)
	return err
}

func (t *Transports) accept(conn *conn) error {
	err := sendAccept(conn.conn, conn.connId)
	if err != nil {
		return err
	}
	conn.mux, err = t.mux.server(conn.conn)
	return err
}

func (t *Transports) handleConn(conn *conn) error {
	if conn.mux == nil {
		return nil
	}
	for {
		c, err := conn.mux.Accept()
		if err != nil {
			conn.Close()
			return err
		}

		s := newStream(c)
		headers, err := t.handshake.Accept(s)
		if err != nil {
			s.Close()
			conn.Close()
			return err
		}
		go t.handle(s, headers)
	}
}
