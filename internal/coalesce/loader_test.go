package coalesce

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLoaderCoalescesConcurrentLoads(t *testing.T) {
	l := New()
	var calls atomicCalls
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			data, err := l.Do(context.Background(), "key", func(context.Context) ([]byte, error) {
				calls.add()
				time.Sleep(20 * time.Millisecond)
				return []byte("payload"), nil
			})
			if err != nil {
				t.Error(err)
				return
			}
			if string(data) != "payload" {
				t.Fatalf("data = %q", data)
			}
		})
	}
	wg.Wait()
	if calls.load() != 1 {
		t.Fatalf("fn called %d times, want 1", calls.load())
	}
}

func TestLoaderWaiterCtxCancel(t *testing.T) {
	l := New()
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = l.Do(context.Background(), "key", func(context.Context) ([]byte, error) {
			<-release
			return []byte("leader"), nil
		})
	}()
	time.Sleep(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := l.Do(ctx, "key", func(context.Context) ([]byte, error) {
		return []byte("waiter"), nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context deadline", err)
	}
	close(release)
	<-done
}

func TestGroupGeneric(t *testing.T) {
	g := NewGroup[int]()
	v, err := g.Do(context.Background(), "k", func(context.Context) (int, error) {
		return 42, nil
	})
	if err != nil || v != 42 {
		t.Fatalf("got %v, %v", v, err)
	}
}

type atomicCalls struct {
	n int
	m sync.Mutex
}

func (a *atomicCalls) add() {
	a.m.Lock()
	a.n++
	a.m.Unlock()
}

func (a *atomicCalls) load() int {
	a.m.Lock()
	defer a.m.Unlock()
	return a.n
}
