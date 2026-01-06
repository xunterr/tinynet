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
	IsClosed() bool
	Close() error
}

type MuxFunc func(net.Conn) (Mux, error)

type asymMux struct {
	client MuxFunc
	server MuxFunc
}

type Option func(*Node)

// set up a muxer
func WithMux(client MuxFunc, server MuxFunc) Option {
	return func(n *Node) {
		n.mux = asymMux{
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
	return func(n *Node) { n.handshake = h }
}

type dialAttempt struct {
	active bool
	ch     chan struct{}
	c      *Conn
	err    error
}

func (d *dialAttempt) notify(c *Conn, err error) {
	if !d.active {
		return
	}
	d.c = c
	d.err = err
	close(d.ch)
	d.active = false
}

type connPool struct {
	sync.Mutex
	p     []*Conn
	limit int
	da    *dialAttempt
}

func (p *connPool) sweep() {
	for _, c := range p.p {
		if !c.IsActive() {
			c.Close()
		}
	}
}

func (p *connPool) pick() (*Conn, bool) {
	for _, c := range p.p {
		if c.IsActive() {
			return c, true
		} else {
			p.delete(c)
		}
	}
	return nil, false
}

func (p *connPool) initDialAttempt() {
	p.da = &dialAttempt{
		ch:     make(chan struct{}),
		active: true,
		c:      nil,
		err:    nil,
	}
}

func (p *connPool) canAppend() bool {
	return len(p.p) < p.limit
}

func (p *connPool) append(c *Conn) bool {
	if !p.canAppend() {
		return false
	}
	p.p = append(p.p, c)
	return true
}

func (p *connPool) delete(c *Conn) bool {
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

type Node struct {
	handleConn func(*Conn)

	handshake Handshake

	nodeId []byte

	connMu         sync.RWMutex
	connections    map[string]*connPool
	maxConnPerNode int

	mux asymMux

	once sync.Once
}

func NewNode(opts ...Option) *Node {
	n := &Node{
		connections:    make(map[string]*connPool),
		handshake:      &DefaultHandshake{},
		maxConnPerNode: 1,
	}

	for _, opt := range opts {
		opt(n)
	}

	return n
}

func makeNodeId(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:16]
}

func (n *Node) SetMux(client MuxFunc, server MuxFunc) {
	n.mux = asymMux{
		client: client,
		server: server,
	}
}

func (n *Node) SetHandleFunc(h func(*Conn)) {
	n.handleConn = h
}

func (n *Node) getOrMakePool(key string) *connPool {
	n.connMu.Lock()
	p, ok := n.connections[key]
	if !ok {
		p = &connPool{
			p:     make([]*Conn, 0, n.maxConnPerNode),
			limit: n.maxConnPerNode,
			da:    &dialAttempt{},
		}
		n.connections[key] = p
	}
	n.connMu.Unlock()
	return p
}

func (n *Node) Dial(addr string) (*Conn, error) {
	p := n.getOrMakePool(string(makeNodeId(addr)))
	p.Lock()
	if p.da.active {
		da := p.da
		p.Unlock()
		<-da.ch
		p.Lock()
	}

	if !p.canAppend() {
		p.Unlock()
		return nil, ErrConnLimit
	}
	p.initDialAttempt()
	p.Unlock()
	return n.dialOnPool(p, addr)
}

func (n *Node) PickOrDial(addr string) (*Conn, error) {
	p := n.getOrMakePool(string(makeNodeId(addr)))
	p.Lock()
	if c, ok := p.pick(); ok {
		p.Unlock()
		<-c.ready
		return c, nil
	}

	var c *Conn
	var err error
	if !p.da.active {
		if !p.canAppend() {
			p.Unlock()
			return nil, ErrConnLimit
		}
		p.initDialAttempt()
		p.Unlock()
		c, err = n.dialOnPool(p, addr)
	} else {
		da := p.da
		p.Unlock()
		<-da.ch
		c, err = da.c, da.err
	}

	return c, err
}

func (n *Node) dialOnPool(p *connPool, addr string) (*Conn, error) {
	c, err := n.createConn(addr) // todo: timeouts
	p.Lock()
	if err == nil {
		p.append(c)
		close(c.ready)
	}
	p.da.notify(c, err)
	p.Unlock()
	return c, err
}

func (n *Node) createConn(addr string) (*Conn, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := n.setupOutConn(c)
	if err != nil {
		c.Close()
		return nil, err
	}

	return conn, nil
}

func (n *Node) setupOutConn(c net.Conn) (*Conn, error) {
	remoteId, err := n.shareId(c)
	if err != nil {
		return nil, err
	}

	connId, err := n.recvHandshake(c)
	if err != nil {
		return nil, err
	}

	// todo: select roles in case of TCP simultaneous open

	mux, err := n.mux.client(c)
	if err != nil {
		return nil, err
	}

	return &Conn{
		node:     n,
		connId:   connId,
		remoteId: remoteId,
		mux:      mux,
		conn:     c,
		ready:    make(chan struct{}),
	}, nil
}

func (n *Node) recvHandshake(c net.Conn) ([]byte, error) {
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

func (n *Node) Listen(addr string) error {
	var err error
	n.once.Do(func() {
		n.nodeId = makeNodeId(addr)
		err = n.listen(addr)
	})
	return err
}

func (n *Node) listen(addr string) error {
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
			conn, err := n.tryAccept(c)
			if err != nil {
				log.Println(err.Error())
				return
			}
			n.handleConn(conn)
		}()
	}
}

func (n *Node) tryAccept(c net.Conn) (*Conn, error) {
	remoteId, err := n.shareId(c)
	if err != nil {
		return nil, err
	}

	p := n.getOrMakePool(string(remoteId))
	p.Lock()
	won := p.da.active && bytes.Compare(remoteId, n.nodeId) <= 0
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
	conn := &Conn{
		node:     n,
		connId:   connIdBytes,
		remoteId: remoteId,
		conn:     c,
		ready:    make(chan struct{}),
	}
	p.append(conn)
	p.Unlock()

	if err = n.accept(conn); err != nil {
		conn.Close()
		p.Lock()
		p.delete(conn)
		p.Unlock()
		return nil, err
	}

	close(conn.ready)
	return conn, nil
}

func (n *Node) deleteConn(c *Conn) {
	n.connMu.RLock()
	p, ok := n.connections[string(c.remoteId)]
	n.connMu.RUnlock()
	if !ok {
		return
	}
	p.Lock()
	p.delete(c)
	p.Unlock()
}

func (n *Node) shareId(c net.Conn) ([]byte, error) {
	_, err := c.Write(n.nodeId)
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

func (n *Node) accept(conn *Conn) error {
	err := sendAccept(conn.conn, conn.connId)
	if err != nil {
		return err
	}
	conn.mux, err = n.mux.server(conn.conn)
	return err
}
