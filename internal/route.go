package internal

import (
	"sync"

	"github.com/xunterr/tinynet/internal/protocol"
)

const (
	RouteKeyHeader uint16 = uint16(protocol.RouteNamespace)<<8 | iota
)

type HandleFunc func(*Stream)

type RoutedTransports struct {
	t        *Node
	handlers map[uint32]HandleFunc
	mu       sync.RWMutex
}

func NewRoutedTransports(t *Node) *RoutedTransports {
	node := &RoutedTransports{
		t:        t,
		handlers: make(map[uint32]HandleFunc),
	}

	return node
}

func (n *RoutedTransports) Handle(key uint32, h HandleFunc) {
	n.mu.Lock()
	n.handlers[key] = h
	n.mu.Unlock()
}

func (r *RoutedTransports) route(key uint32) HandleFunc {
	r.mu.RLock()
	h, ok := r.handlers[key]
	r.mu.RUnlock()
	if !ok {
		return nil
	}

	return h
}

func (n *RoutedTransports) Listen(addr string) error {
	return n.t.Listen(addr)
}

func (n *RoutedTransports) Dial(addr string, key uint32) (*Conn, error) {
	conn, err := n.t.Dial(addr)
	if err != nil {
		return nil, err
	}

	return conn, nil
}
