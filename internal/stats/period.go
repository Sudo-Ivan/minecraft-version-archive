package stats

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"minecraft-version-archive/internal/constants"
)

type PeriodSnapshot struct {
	BytesOut uint64
	BytesIn  uint64
	Requests uint64
	Blocked  uint64
	Errors   uint64
	Clients  int
}

const clientSetShards = 32

type clientShard struct {
	mu      sync.Mutex
	clients map[string]struct{}
}

type periodTracker struct {
	bytesIn  atomic.Uint64
	requests atomic.Uint64
	blocked  atomic.Uint64
	errors   atomic.Uint64
	shards   [clientSetShards]clientShard
}

func newPeriodTracker() *periodTracker {
	p := &periodTracker{}
	for i := range p.shards {
		p.shards[i].clients = make(map[string]struct{})
	}
	return p
}

func ipShard(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}

func (p *periodTracker) recordRequest(bytesIn int64, client string) {
	if bytesIn > 0 {
		p.bytesIn.Add(uint64(bytesIn))
	}
	p.requests.Add(1)
	if client == "" {
		return
	}
	sh := &p.shards[ipShard(client)%clientSetShards]
	sh.mu.Lock()
	if len(sh.clients) < constants.StatsMaxTrackedClients/clientSetShards {
		sh.clients[client] = struct{}{}
	}
	sh.mu.Unlock()
}

func (p *periodTracker) recordError() {
	p.errors.Add(1)
}

func (p *periodTracker) recordBlocked() {
	p.blocked.Add(1)
}

func (p *periodTracker) flush(bytesOut uint64) PeriodSnapshot {
	snap := PeriodSnapshot{
		BytesOut: bytesOut,
		BytesIn:  p.bytesIn.Swap(0),
		Requests: p.requests.Swap(0),
		Blocked:  p.blocked.Swap(0),
		Errors:   p.errors.Swap(0),
	}
	for i := range p.shards {
		sh := &p.shards[i]
		sh.mu.Lock()
		snap.Clients += len(sh.clients)
		sh.clients = make(map[string]struct{})
		sh.mu.Unlock()
	}
	return snap
}

func FormatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func FormatPeriodLine(snap PeriodSnapshot) string {
	return fmt.Sprintf(
		"mva: clients=%d requests=%d blocked=%d errors=%d out=%s in=%s",
		snap.Clients,
		snap.Requests,
		snap.Blocked,
		snap.Errors,
		FormatBytes(snap.BytesOut),
		FormatBytes(snap.BytesIn),
	)
}

type PeriodLineOpts struct {
	Refresh   bool
	LastWidth int
}

func WritePeriodLine(w io.Writer, snap PeriodSnapshot, opts PeriodLineOpts) (int, error) {
	line := FormatPeriodLine(snap)
	if opts.Refresh {
		pad := ""
		if opts.LastWidth > len(line) {
			pad = strings.Repeat(" ", opts.LastWidth-len(line))
		}
		_, err := fmt.Fprintf(w, "\r%s%s", line, pad)
		return len(line), err
	}
	_, err := fmt.Fprintf(w, "%s\n", line)
	return len(line), err
}
