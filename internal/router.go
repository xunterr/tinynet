package internal

import (
	"sync"
)

type DefaultRouter struct {
	handlers map[uint32]HandleFunc
	mu       sync.RWMutex
}

func NewDefaultRouter() *DefaultRouter {
	return &DefaultRouter{
		handlers: make(map[uint32]HandleFunc),
	}
}

func (r *DefaultRouter) Handle(key uint32, h HandleFunc) {
	r.mu.Lock()
	r.handlers[key] = h
	r.mu.Unlock()
}

func (r *DefaultRouter) Route(key uint32) HandleFunc {
	r.mu.RLock()
	h, ok := r.handlers[key]
	r.mu.RUnlock()
	if !ok {
		return nil
	}

	return h
}
