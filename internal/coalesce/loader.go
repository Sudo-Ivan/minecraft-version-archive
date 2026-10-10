package coalesce

import (
	"context"
	"sync"
)

type call[T any] struct {
	done chan struct{}
	data T
	err  error
	dups int
}

type Group[T any] struct {
	mu       sync.Mutex
	inflight map[string]*call[T]
}

func NewGroup[T any]() *Group[T] {
	return &Group[T]{inflight: make(map[string]*call[T])}
}

func (g *Group[T]) Do(ctx context.Context, key string, fn func(context.Context) (T, error)) (T, error) {
	g.mu.Lock()
	if c, ok := g.inflight[key]; ok {
		c.dups++
		g.mu.Unlock()
		select {
		case <-c.done:
			return c.data, c.err
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
	c := &call[T]{done: make(chan struct{})}
	g.inflight[key] = c
	g.mu.Unlock()

	c.data, c.err = fn(ctx)
	close(c.done)

	g.mu.Lock()
	delete(g.inflight, key)
	g.mu.Unlock()
	return c.data, c.err
}

type Loader = Group[[]byte]

func New() *Loader {
	return NewGroup[[]byte]()
}
