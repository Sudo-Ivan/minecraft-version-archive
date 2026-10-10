package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"minecraft-version-archive/internal/constants"
)

func TestCollectorFileStore(t *testing.T) {
	dir := t.TempDir()
	c := New()
	if err := c.EnableFileStore(dir); err != nil {
		t.Fatal(err)
	}
	c.RecordRequest(100, "10.0.0.1")
	c.RecordRequest(200, "10.0.0.2")
	c.RecordRequest(50, "10.0.0.1")

	snap := c.FlushPeriod(0)
	if snap.Clients != 2 {
		t.Fatalf("Clients = %d", snap.Clients)
	}
	if snap.Requests != 3 {
		t.Fatalf("Requests = %d", snap.Requests)
	}

	history := filepath.Join(dir, statsDirName, "history.log")
	if _, err := os.Stat(history); os.IsNotExist(err) {
		line := FormatPeriodLine(snap)
		if err := c.FileStore().AppendHistory(line, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty history")
	}
}

func TestEnableFileStoreRecordError(t *testing.T) {
	c := New()
	c.RecordError()
	snap := c.FlushPeriod(0)
	if snap.Errors != 1 {
		t.Fatalf("Errors = %d", snap.Errors)
	}
}

func TestHistoryRotation(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	line := "2026-01-01T00:00:00Z requests=100"
	lineBytes := len(line) + len(" ") + len("2026-01-01T00:00:00Z") + 1
	n := constants.StatsHistoryMaxBytes/lineBytes + 16
	for i := 0; i < n; i++ {
		if err := fs.AppendHistory(line, time.Unix(0, 0)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(fs.history)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > constants.StatsHistoryMaxBytes {
		t.Fatalf("history.log not rotated: %d bytes", info.Size())
	}
	data, err := os.ReadFile(fs.history)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[0] == '\n' || data[len(data)-1] != '\n' {
		t.Fatal("rotated file has bad line boundaries")
	}
}
