package internal

import (
	"github.com/xunterr/tinynet/internal/transports"
)

type Handshake interface {
	Accept(*transports.Stream) (uint32, error)
	Propose(*transports.Stream, uint32) error
}

type HandleFunc func(*transports.Stream)

type Router interface {
	Route(uint32) HandleFunc
}

type Node struct {
	t         *transports.Transports
	handshake Handshake
	router    Router
}

type Option func(*Node)

func WithTransports(t *transports.Transports) Option {
	return func(n *Node) {
		n.t = t
	}
}

func WithHandshake(h Handshake) Option {
	return func(n *Node) {
		n.handshake = h
	}
}

func NewNode(r Router, opts ...Option) *Node {
	t := transports.NewTransports()

	node := &Node{
		t:         t,
		handshake: &DefaultHandshake{},
		router:    r,
	}

	t.SetHandleFunc(node.handle)
	return node
}

func (n *Node) handle(stream *transports.Stream) {
	k, err := n.handshake.Accept(stream)
	if err != nil {
		return
	}

	h := n.router.Route(k)
	if h != nil {
		h(stream)
	}
}

func (n *Node) Listen(addr string) error {
	return n.t.Listen(addr)
}

func (n *Node) Dial(addr string, key uint32) (*transports.Stream, error) {
	stream, err := n.t.Dial(addr)
	if err != nil {
		return nil, err
	}

	err = n.handshake.Propose(stream, key)
	if err != nil {
		return nil, err
	}

	return stream, nil
}
