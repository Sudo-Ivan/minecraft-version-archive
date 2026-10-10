package archive

import (
	"testing"
)

func TestProgressCounters(t *testing.T) {
	p := NewProgress(3, nil)
	p.BeginVersion("1.0")
	p.AddTasks(10)
	p.TaskDone(100)
	p.TaskDone(200)
	p.EndVersion("1.0", true)

	if p.versionsDone.Load() != 1 {
		t.Fatalf("versionsDone = %d", p.versionsDone.Load())
	}
	if p.filesDone.Load() != 2 {
		t.Fatalf("filesDone = %d", p.filesDone.Load())
	}
	if p.bytesDone.Load() != 300 {
		t.Fatalf("bytesDone = %d", p.bytesDone.Load())
	}
}

func TestProgressActiveTracking(t *testing.T) {
	p := NewProgress(2, nil)
	p.BeginVersion("a")
	p.BeginVersion("b")
	if p.activeCount() != 2 {
		t.Fatalf("active = %d", p.activeCount())
	}
	p.EndVersion("a", true)
	if p.activeCount() != 1 {
		t.Fatalf("active = %d", p.activeCount())
	}
}
