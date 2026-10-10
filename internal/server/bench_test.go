package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkParseRange(b *testing.B) {
	header := "bytes=100-999"
	size := 4096
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseRange(header, size)
	}
}

func BenchmarkParseRangeSuffix(b *testing.B) {
	header := "bytes=-512"
	size := 4096
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseRange(header, size)
	}
}

func BenchmarkServeBytesFull(b *testing.B) {
	data := make([]byte, 4096)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Body.Reset()
		serveBytes(rec, r, data)
	}
}

func BenchmarkServeBytesRange(b *testing.B) {
	data := make([]byte, 4096)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "bytes=0-511")
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Body.Reset()
		serveBytes(rec, r, data)
	}
}

func BenchmarkFormatInt(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		formatInt(i % 1000000)
	}
}
