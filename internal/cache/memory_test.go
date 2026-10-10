package cache

import (
	"fmt"
	"sync"
	"testing"

	"minecraft-version-archive/internal/constants"
)

func TestLRUEvictionByEntries(t *testing.T) {
	c := NewSharded(2, 1024, 1)
	c.Put("a", []byte("aaa"))
	c.Put("b", []byte("bbb"))
	c.Put("c", []byte("ccc"))

	if _, ok := c.Get("a"); ok {
		t.Fatal("a should have been evicted")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("b should remain")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("c should remain")
	}
}

func TestLRUEvictionByBytes(t *testing.T) {
	c := NewSharded(100, 10, 1)
	c.Put("a", []byte("12345"))
	c.Put("b", []byte("12345"))
	c.Put("c", []byte("12345"))

	entries, bytes := c.Stats()
	if entries > 2 {
		t.Fatalf("entries = %d, bytes = %d", entries, bytes)
	}
	if bytes > 10 {
		t.Fatalf("bytes = %d exceed max 10", bytes)
	}
}

func TestGetMovesToFront(t *testing.T) {
	c := NewSharded(2, 1024, 1)
	c.Put("a", []byte("1"))
	c.Put("b", []byte("2"))
	for range constants.LRUTouchInterval {
		c.Get("a")
	}
	c.Put("c", []byte("3"))

	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted after a was touched")
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New(64, 1024*1024)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n%10)
			c.Put(key, []byte("payload"))
			c.Get(key)
			c.Stats()
		}(i)
	}
	wg.Wait()
}
