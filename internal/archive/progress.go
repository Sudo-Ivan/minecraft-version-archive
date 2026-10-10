package archive

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type Progress struct {
	versionsTotal int
	versionsDone  atomic.Int32
	filesTotal    atomic.Int64
	filesDone     atomic.Int64
	bytesDone     atomic.Int64
	active        sync.Map
	started       time.Time
	log           *slog.Logger
}

func NewProgress(total int, log *slog.Logger) *Progress {
	return &Progress{
		versionsTotal: total,
		started:       time.Now(),
		log:           log,
	}
}

func (p *Progress) Run(ctx context.Context) {
	if p.log == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.logStatus(false)
		}
	}
}

func (p *Progress) BeginVersion(id string) {
	p.active.Store(id, struct{}{})
	if p.log != nil {
		p.logStatus(false)
	}
}

func (p *Progress) EndVersion(id string, ok bool) {
	p.active.Delete(id)
	if ok {
		p.versionsDone.Add(1)
	}
	if p.log != nil {
		p.logStatus(true)
	}
}

func (p *Progress) AddTasks(n int64) {
	p.filesTotal.Add(n)
}

func (p *Progress) TaskDone(bytes int64) {
	p.filesDone.Add(1)
	if bytes > 0 {
		p.bytesDone.Add(bytes)
	}
}

func (p *Progress) activeCount() int {
	n := 0
	p.active.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

func (p *Progress) activeVersions() []string {
	var ids []string
	p.active.Range(func(key, _ any) bool {
		ids = append(ids, key.(string))
		return true
	})
	return ids
}

func (p *Progress) logStatus(force bool) {
	done := int(p.versionsDone.Load())
	filesDone := p.filesDone.Load()
	filesTotal := p.filesTotal.Load()
	active := p.activeCount()

	if !force && done == 0 && filesDone == 0 && active == 0 {
		return
	}

	elapsed := time.Since(p.started).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(p.bytesDone.Load()) / elapsed
	}

	filePct := 0.0
	if filesTotal > 0 {
		filePct = float64(filesDone) / float64(filesTotal) * 100
	}

	attrs := []any{
		"versions_done", done,
		"versions_total", p.versionsTotal,
		"versions_active", active,
		"files_done", filesDone,
		"files_total", filesTotal,
		"files_pct", filePct,
		"bytes", p.bytesDone.Load(),
		"speed_bps", speed,
		"final", force,
	}
	if len(p.activeVersions()) > 0 {
		attrs = append(attrs, "working", p.activeVersions())
	}
	p.log.Info("progress", attrs...)
}
