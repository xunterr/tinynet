package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
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

// Function to be called on a connection
type SetupFunc func(net.Conn) (net.Conn, error)

type connSetup struct {
	open   SetupFunc
	accept SetupFunc
}

type Option func(*Node)

// Set up a muxer
func NewMux(client MuxFunc, server MuxFunc) asymMux {
	return asymMux{
		client: client,
		server: server,
	}
}

// Set up a symmetrical muxer (client and server muxers are the same)
func SymMux(m MuxFunc) (MuxFunc, MuxFunc) {
	return m, m
}

// Set up a functions to be called on a connection setup
func WithSetup(open SetupFunc, accept SetupFunc) Option {
	return func(n *Node) {
		n.setup = connSetup{open, accept}
	}
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
	l          net.Listener

	nodeId []byte

	connMu         sync.RWMutex
	connections    map[string]*connPool
	maxConnPerNode int

	mux asymMux

	setup connSetup

	once sync.Once
}

var NopSetup = connSetup{
	func(c net.Conn) (net.Conn, error) { return c, nil },
	func(c net.Conn) (net.Conn, error) { return c, nil },
}

func NewNode(mux asymMux, opts ...Option) *Node {
	n := &Node{
		connections:    make(map[string]*connPool),
		maxConnPerNode: 1,
		setup:          NopSetup,
		mux:            mux,
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

func tcpDial(addr string) (net.Conn, error) {
	return net.Dial("tcp", addr)
}

func (node *Node) createConn(addr string) (*Conn, error) {
	c, err := tcpDial(addr)
	if err != nil {
		return nil, err
	}

	h, err := node.initHandshake(c)
	if err != nil {
		return nil, err
	}

	c, err = node.setup.open(c)
	if err != nil {
		return nil, err
	}

	mux, err := node.mux.client(c)
	if err != nil {
		return nil, err
	}

	return &Conn{
		node:     node,
		connId:   h.connId,
		remoteId: h.remoteId,
		mux:      mux,
		conn:     c,
		ready:    make(chan struct{}),
	}, nil
}

type servHandshake struct {
	status   byte
	ver      byte
	connId   []byte
	remoteId []byte
}

func (node *Node) initHandshake(c net.Conn) (servHandshake, error) {
	_, err := node.sendInit(c)
	if err != nil {
		return servHandshake{}, err
	}

	buf := make([]byte, ConnInitMsgSize+1)
	_, err = io.ReadFull(c, buf)
	if err != nil {
		return servHandshake{}, err
	}

	n, ver, remoteId := readInit(buf)
	h := servHandshake{
		ver:      ver,
		status:   buf[n],
		remoteId: remoteId,
	}

	if buf[n] == ConnRejected {
		return h, ErrHandshakeRefused
	}

	connId := make([]byte, 16)
	_, err = io.ReadFull(c, connId)
	if err != nil {
		return servHandshake{}, err
	}

	h.connId = connId
	return h, nil
}

func (n *Node) Listen(addr string) error {
	var err error
	n.once.Do(func() {
		n.nodeId = makeNodeId(addr)
		err = n.listen(addr)
	})
	return err
}

func (n *Node) Close() error {
	return n.l.Close()
}

func (n *Node) listen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	n.l = l
	return nil
}

func (n *Node) Accept() (*Conn, error) {
	c, err := n.l.Accept()
	if err != nil {
		return nil, err
	}
	return n.tryAccept(c)
}

func (n *Node) tryAccept(c net.Conn) (*Conn, error) {
	_, remoteId, err := n.recvInit(c)
	if err != nil {
		return nil, err
	}

	p := n.getOrMakePool(string(remoteId))
	p.Lock()
	won := p.da.active && bytes.Compare(remoteId, n.nodeId) <= 0
	if won { // if there are concurrent requests AND we won
		p.Unlock()
		n.reject(c)
		c.Close()
		return nil, ErrConcurrent
	}
	if !p.canAppend() {
		p.Unlock()
		n.reject(c)
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

func (n *Node) sendInit(w io.Writer) (int, error) {
	buf := make([]byte, ConnInitMsgSize)
	putInit(buf, 0, n.nodeId)
	return w.Write(buf)
}

func (n *Node) recvInit(r io.Reader) (ver byte, id []byte, err error) {
	buf := make([]byte, ConnInitMsgSize)
	_, err = io.ReadFull(r, buf)
	if err != nil {
		return 0, nil, err
	}
	_, ver, id = readInit(buf)
	return
}

func (n *Node) reject(c net.Conn) error {
	buf := make([]byte, ConnInitMsgSize+1)
	i := putInit(buf, 0, n.nodeId)
	i = putReject(buf[i:])
	_, err := c.Write(buf)
	return err
}

func (n *Node) accept(conn *Conn) error {
	buf := make([]byte, ConnInitMsgSize+17)

	i := putInit(buf, 0, n.nodeId)
	i = putAccept(buf[i:], conn.connId)
	_, err := conn.conn.Write(buf)
	if err != nil {
		return err
	}

	conn.conn, err = n.setup.open(conn.conn)
	if err != nil {
		return err
	}
	conn.mux, err = n.mux.server(conn.conn)
	return err
}

// 		1			|		16
// --------------------
//	ver  |	 nodeId

// 		1			|		16		|		1	  	|		16		|
// --------------------------------------
//	ver  |	 nodeId	 |	accept |	connId |

const (
	ConnInitMsgSize int = 17
)

func putAccept(p []byte, connId []byte) int {
	if len(p) == 0 {
		return 0
	}
	p[0] = ConnAccepted
	end := min(len(connId)+1, len(p))
	copy(p[1:end], connId)
	return end
}

func putReject(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	p[0] = ConnRejected
	return 1
}

// dups now, layout may change

func putInit(p []byte, ver byte, id []byte) int {
	if len(p) == 0 {
		return 0
	}
	n := ConnInitMsgSize
	if len(p) < ConnInitMsgSize {
		n = len(p)
	}
	p[0] = ver
	copy(p[1:n], id)
	return n
}

func readInit(p []byte) (n int, ver byte, nodeId []byte) {
	if len(p) == 0 {
		return 0, 0, nil
	}
	n = ConnInitMsgSize
	if len(p) < ConnInitMsgSize {
		n = len(p)
	}
	ver = p[0]
	if n > 1 {
		nodeId = p[1:n]
	}
	return
}
