package stats

import "testing"

func BenchmarkRecordServe(b *testing.B) {
	c := New()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.RecordServe(4096, i%2 == 0)
	}
}

func BenchmarkSnapshot(b *testing.B) {
	c := New()
	c.RecordServe(1024, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Snapshot(10, 1024, 4096, 5)
	}
}
