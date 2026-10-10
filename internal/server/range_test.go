package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseRangeFull(t *testing.T) {
	start, end, ok := parseRange("bytes=0-", 100)
	if !ok || start != 0 || end != 99 {
		t.Fatalf("got %d-%d ok=%v", start, end, ok)
	}
}

func TestParseRangeExplicit(t *testing.T) {
	start, end, ok := parseRange("bytes=10-19", 100)
	if !ok || start != 10 || end != 19 {
		t.Fatalf("got %d-%d ok=%v", start, end, ok)
	}
}

func TestParseRangeSuffix(t *testing.T) {
	start, end, ok := parseRange("bytes=-10", 100)
	if !ok || start != 90 || end != 99 {
		t.Fatalf("got %d-%d ok=%v", start, end, ok)
	}
}

func TestParseRangeInvalid(t *testing.T) {
	cases := []string{
		"invalid",
		"bytes=200-100",
		"bytes=500-",
		"bytes=0-200",
	}
	for _, c := range cases {
		if _, _, ok := parseRange(c, 100); ok {
			t.Fatalf("expected invalid range for %q", c)
		}
	}
}

func TestServeBytesRange(t *testing.T) {
	rec := &recordingWriter{header: make(map[string][]string)}
	data := []byte("0123456789")
	n := serveBytes(rec, newRangeRequest("bytes=2-5"), data)
	if n != 4 {
		t.Fatalf("written = %d", n)
	}
	if string(rec.body) != "2345" {
		t.Fatalf("body = %q", rec.body)
	}
	if rec.status != 206 {
		t.Fatalf("status = %d", rec.status)
	}
}

func TestServeBytesFull(t *testing.T) {
	rec := &recordingWriter{header: make(map[string][]string)}
	data := []byte("hello")
	n := serveBytes(rec, newRangeRequest(""), data)
	if n != 5 || string(rec.body) != "hello" {
		t.Fatalf("n=%d body=%q", n, rec.body)
	}
}

type recordingWriter struct {
	header map[string][]string
	status int
	body   []byte
}

func (w *recordingWriter) Header() http.Header { return w.header }
func (w *recordingWriter) Write(p []byte) (int, error) {
	w.body = append(w.body, p...)
	return len(p), nil
}
func (w *recordingWriter) WriteHeader(status int) { w.status = status }

func newRangeRequest(rangeHeader string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if rangeHeader != "" {
		r.Header.Set("Range", rangeHeader)
	}
	return r
}
