package stats

import (
	"bytes"
	"testing"
)

func TestFlushPeriod(t *testing.T) {
	c := New()
	c.RecordRequest(500, "1.2.3.4")
	c.RecordRequest(300, "1.2.3.4")
	c.RecordRequest(200, "5.6.7.8")
	c.RecordBlocked()
	c.RecordError()

	snap := c.FlushPeriod(10_000)
	if snap.BytesOut != 10_000 {
		t.Fatalf("BytesOut = %d", snap.BytesOut)
	}
	if snap.BytesIn != 1000 {
		t.Fatalf("BytesIn = %d", snap.BytesIn)
	}
	if snap.Requests != 3 {
		t.Fatalf("Requests = %d", snap.Requests)
	}
	if snap.Blocked != 1 {
		t.Fatalf("Blocked = %d", snap.Blocked)
	}
	if snap.Errors != 1 {
		t.Fatalf("Errors = %d", snap.Errors)
	}
	if snap.Clients != 2 {
		t.Fatalf("Clients = %d", snap.Clients)
	}

	empty := c.FlushPeriod(0)
	if empty.Requests != 0 || empty.Clients != 0 {
		t.Fatalf("expected empty period after flush, got %+v", empty)
	}
}

func TestWritePeriodLine(t *testing.T) {
	var buf bytes.Buffer
	if _, err := WritePeriodLine(&buf, PeriodSnapshot{
		Clients:  12,
		Requests: 340,
		Blocked:  5,
		Errors:   2,
		BytesOut: 1 << 20,
		BytesIn:  4096,
	}, PeriodLineOpts{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("clients=12")) {
		t.Fatalf("line = %q", buf.Bytes())
	}
	if !bytes.Contains(buf.Bytes(), []byte("errors=2")) {
		t.Fatalf("line = %q", buf.Bytes())
	}
}

func TestWritePeriodLineRefresh(t *testing.T) {
	var buf bytes.Buffer
	width, err := WritePeriodLine(&buf, PeriodSnapshot{
		Clients:  1,
		Requests: 2,
		BytesOut: 100,
	}, PeriodLineOpts{Refresh: true, LastWidth: 80})
	if err != nil {
		t.Fatal(err)
	}
	if width == 0 {
		t.Fatal("expected width")
	}
	if buf.Bytes()[0] != '\r' {
		t.Fatalf("line = %q", buf.Bytes())
	}
}

func TestFormatBytes(t *testing.T) {
	if FormatBytes(512) != "512 B" {
		t.Fatal(FormatBytes(512))
	}
	if FormatBytes(1536) != "1.5 KiB" {
		t.Fatal(FormatBytes(1536))
	}
}
