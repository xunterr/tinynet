package internal

import (
	"encoding/binary"
	"sync"

	"github.com/xunterr/tinynet/internal/protocol"
)

const (
	RouteKeyHeader uint16 = uint16(protocol.RouteNamespace)<<8 | iota
)

type HandleFunc func(*Stream)

type RoutedTransports struct {
	t        *Transports
	handlers map[uint32]HandleFunc
	mu       sync.RWMutex
}

func NewRoutedTransports(t *Transports) *RoutedTransports {
	node := &RoutedTransports{
		t:        t,
		handlers: make(map[uint32]HandleFunc),
	}

	t.SetHandleFunc(node.handle)
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

func (n *RoutedTransports) handle(stream *Stream, headers Headers) {
	h, ok := headers.getHeader(RouteKeyHeader)
	if !ok {
		stream.Close()
		return
	}
	routeKey := binary.BigEndian.Uint32(h)
	handler := n.route(routeKey)
	if handler != nil {
		handler(stream)
	}
}

func (n *RoutedTransports) Listen(addr string) error {
	return n.t.Listen(addr)
}

func (n *RoutedTransports) Dial(addr string, key uint32) (*Stream, error) {
	var keyBytes [4]byte
	binary.BigEndian.PutUint32(keyBytes[:], key)
	header := Header{Type: RouteKeyHeader, Value: keyBytes[:]}
	stream, err := n.t.Dial(addr, header)
	if err != nil {
		return nil, err
	}

	return stream, nil
}
