package humanlog

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestFormatProgress(t *testing.T) {
	var buf bytes.Buffer
	h := New(&buf)
	h.color = false
	h.isTTY = false

	logger := slog.New(h)
	logger.Info("progress",
		"versions_done", 0,
		"versions_total", 1,
		"versions_active", 1,
		"files_done", int64(186),
		"files_total", int64(4860),
		"files_pct", 3.8,
		"bytes", int64(48934102),
		"speed_bps", 24458118.0,
		"working", []string{"26.1.2"},
	)

	got := strings.TrimSpace(buf.String())
	want := "Progress 186/4860 files (3.8%)  |  46.7 MB  |  23.3 MB/s  |  26.1.2"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatFabricArchivePlan(t *testing.T) {
	var buf bytes.Buffer
	h := New(&buf)
	h.color = false

	logger := slog.New(h)
	logger.Info("fabric archive plan", "minecraft", 1, "loaders", 1, "all_loaders", false, "workers", 8)

	got := strings.TrimSpace(buf.String())
	want := "Plan 1 minecraft  |  1 loaders  |  8 workers"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatFabricArchiveComplete(t *testing.T) {
	var buf bytes.Buffer
	h := New(&buf)
	h.color = false

	logger := slog.New(h)
	logger.Info("fabric archive complete", "minecraft", 1, "loaders", 1)

	got := strings.TrimSpace(buf.String())
	want := "Done fabric archived 1 minecraft  |  1 loader(s)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatArchivePlan(t *testing.T) {
	var buf bytes.Buffer
	h := New(&buf)
	h.color = false

	logger := slog.New(h)
	logger.Info("archive plan", "agent", "dev-1", "workers", 8, "pending", 1, "total", 1, "skipped", 0)

	got := strings.TrimSpace(buf.String())
	want := "Plan 1 to download  |  1 total  |  0 skipped  |  8 workers  |  agent dev-1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestProgressInplaceOnTTY(t *testing.T) {
	var buf bytes.Buffer
	h := &Handler{w: &buf, color: false, isTTY: true}
	logger := slog.New(h)
	logger.Info("progress",
		"versions_done", 0,
		"versions_total", 1,
		"files_done", int64(1),
		"files_total", int64(10),
		"files_pct", 10.0,
		"bytes", int64(100),
		"speed_bps", 50.0,
		"final", false,
	)
	if !bytes.HasPrefix(buf.Bytes(), []byte("\r")) {
		t.Fatalf("expected inplace progress, got %q", buf.String())
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		512:        "512 B",
		1536:       "1.5 KB",
		48934102:   "46.7 MB",
		1073741824: "1.0 GB",
	}
	for n, want := range cases {
		if got := formatBytes(n); got != want {
			t.Fatalf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
