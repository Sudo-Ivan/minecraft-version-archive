package coordinator

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestClaimNextNoOverlap(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "agent-a", 3600)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(dir, "agent-b", 3600)
	if err != nil {
		t.Fatal(err)
	}

	versions := []string{"1.20.1", "1.19.4", "1.18.2"}
	claimed := make(map[string]string)
	active := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	deadline := time.Now().Add(10 * time.Second)
	errCh := make(chan error, 2)

	for _, agent := range []*Coordinator{a, b} {
		wg.Add(1)
		go func(c *Coordinator) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				id, ok := c.ClaimNext(versions)
				if !ok {
					if allDone(versions, c) {
						return
					}
					time.Sleep(5 * time.Millisecond)
					continue
				}
				mu.Lock()
				if owner, exists := active[id]; exists {
					mu.Unlock()
					errCh <- fmt.Errorf("version %s claimed by %s and %s", id, owner, c.agent)
					return
				}
				active[id] = c.agent
				claimed[id] = c.agent
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				if err := c.MarkDone(id); err != nil {
					errCh <- err
					return
				}
				c.Release(id)
				mu.Lock()
				delete(active, id)
				mu.Unlock()
			}
			errCh <- fmt.Errorf("timed out waiting for versions")
		}(agent)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}

	if len(claimed) != len(versions) {
		t.Fatalf("claimed %d versions, want %d", len(claimed), len(versions))
	}
}

func allDone(versions []string, c *Coordinator) bool {
	for _, id := range versions {
		if !c.IsDone(id) {
			return false
		}
	}
	return true
}

func TestStaleClaimRecovery(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, "agent-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := c.ClaimNext([]string{"1.0"})
	if !ok {
		t.Fatal("expected claim")
	}
	if id != "1.0" {
		t.Fatalf("id = %q", id)
	}
	time.Sleep(1100 * time.Millisecond)
	b, err := New(dir, "agent-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	id2, ok := b.ClaimNext([]string{"1.0"})
	if !ok || id2 != "1.0" {
		t.Fatalf("expected stale reclaim, got %q ok=%v", id2, ok)
	}
	b.Release(id2)
}

func TestPendingSkipsDone(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, "agent", 3600)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.MarkDone("1.20.1"); err != nil {
		t.Fatal(err)
	}
	pending := Pending([]string{"1.20.1", "1.19.4"}, c)
	if len(pending) != 1 || pending[0] != "1.19.4" {
		t.Fatalf("pending = %v", pending)
	}
}
