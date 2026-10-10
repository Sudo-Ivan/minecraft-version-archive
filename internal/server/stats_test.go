package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

func TestStartPeriodicStatsRefreshLine(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
	}
	log := slog.New(slog.DiscardHandler)
	st := stats.New()
	if err := st.EnableFileStore(dir); err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, archive.NewWithStats(cfg, log, st), log)

	var buf syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.StartPeriodicStats(ctx, &buf, 20*time.Millisecond)

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if bytes.Contains(buf.Bytes(), []byte("clients=0")) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	time.Sleep(30 * time.Millisecond)

	out := buf.Bytes()
	if len(out) == 0 {
		t.Fatal("no stats output")
	}
	if out[0] != '\r' {
		t.Fatalf("expected refresh prefix, got %q", out)
	}
	if bytes.Count(out, []byte("\n")) != 1 {
		t.Fatalf("expected single trailing newline on shutdown, got %d newlines in %q", bytes.Count(out, []byte("\n")), out)
	}
}

func TestRecordProxyError(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
		ProxyOnMiss:  true,
	}
	log := slog.New(slog.DiscardHandler)
	st := stats.New()
	srv := New(cfg, archive.NewWithStats(cfg, log, st), log)

	srv.recordProxyError("test proxy", errors.New("upstream failed"), "path", "/missing")

	period := st.FlushPeriod(0)
	if period.Errors != 1 {
		t.Fatalf("period Errors = %d", period.Errors)
	}
	snap := st.Snapshot(0, 0, 0, 0)
	if snap.Errors != 1 {
		t.Fatalf("Errors = %d", snap.Errors)
	}
}
