package cache

import (
	"fmt"
	"testing"
)

func BenchmarkCacheGetHitParallel(b *testing.B) {
	c := NewSharded(4096, 1024*1024, 256)
	c.Put("key", make([]byte, 4096))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Get("key")
		}
	})
}

func BenchmarkCacheGetHit(b *testing.B) {
	c := New(64, 1024*1024)
	c.Put("key", make([]byte, 4096))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get("key")
	}
}

func BenchmarkCachePut(b *testing.B) {
	c := New(1024, 64*1024*1024)
	data := make([]byte, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Put(fmt.Sprintf("key-%d", i%512), data)
	}
}

func BenchmarkCachePutUpdate(b *testing.B) {
	c := New(64, 1024*1024)
	data := make([]byte, 4096)
	c.Put("key", data)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Put("key", data)
	}
}
