package stats

import (
	"bytes"
	"testing"
	"time"
)

func TestRecordServeAndSnapshot(t *testing.T) {
	c := New()
	c.RecordServe(1000, true)
	c.RecordServe(2000, false)

	snap := c.Snapshot(3, 3000, 10000, 5)
	if snap.TotalRequests != 2 {
		t.Fatalf("TotalRequests = %d", snap.TotalRequests)
	}
	if snap.TotalBytesServed != 3000 {
		t.Fatalf("TotalBytesServed = %d", snap.TotalBytesServed)
	}
	if snap.CacheHits != 1 || snap.CacheMisses != 1 {
		t.Fatalf("hits=%d misses=%d", snap.CacheHits, snap.CacheMisses)
	}
	if snap.CacheEntries != 3 || snap.VersionsStored != 5 {
		t.Fatalf("cache=%d versions=%d", snap.CacheEntries, snap.VersionsStored)
	}
}

func TestRecentRate(t *testing.T) {
	c := New()
	c.RecordServe(10000, false)
	time.Sleep(20 * time.Millisecond)
	rate := c.Snapshot(0, 0, 0, 0).ServeBytesPerSec
	if rate <= 0 {
		t.Fatalf("ServeBytesPerSec = %f", rate)
	}
}

func TestWriteJSON(t *testing.T) {
	c := New()
	var buf bytes.Buffer
	if err := c.WriteJSON(&buf, 1, 100, 200, 2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"total_requests"`)) {
		t.Fatalf("json = %s", buf.Bytes())
	}
}

func TestConcurrentRecord(t *testing.T) {
	c := New()
	done := make(chan struct{})
	go func() {
		for i := range 1000 {
			c.RecordServe(100, i%2 == 0)
		}
		close(done)
	}()
	for range 1000 {
		c.RecordArchiveDownload(50)
	}
	<-done
	snap := c.Snapshot(0, 0, 0, 0)
	if snap.TotalRequests != 1000 {
		t.Fatalf("TotalRequests = %d", snap.TotalRequests)
	}
	if snap.ArchiveDownloads != 1000 {
		t.Fatalf("ArchiveDownloads = %d", snap.ArchiveDownloads)
	}
}
