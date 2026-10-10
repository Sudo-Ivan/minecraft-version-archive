package stats

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"minecraft-version-archive/internal/constants"
)

type Collector struct {
	started       time.Time
	totalRequests atomic.Uint64
	totalBytes    atomic.Uint64
	totalBytesIn  atomic.Uint64
	cacheHits     atomic.Uint64
	cacheMisses   atomic.Uint64
	blockedTotal  atomic.Uint64
	errorsTotal   atomic.Uint64
	archiveFiles  atomic.Uint64
	archiveBytes  atomic.Uint64
	recentIdx     atomic.Uint64
	recentAt      [constants.StatsRingSize]atomic.Int64
	recentBytes   [constants.StatsRingSize]atomic.Uint64
	period        *periodTracker
	files         *FileStore
}

type Snapshot struct {
	UptimeSec        float64 `json:"uptime_sec"`
	TotalRequests    uint64  `json:"total_requests"`
	TotalBytesServed uint64  `json:"total_bytes_served"`
	TotalBytesIn     uint64  `json:"total_bytes_in"`
	BlockedRequests  uint64  `json:"blocked_requests"`
	Errors           uint64  `json:"errors"`
	CacheHits        uint64  `json:"cache_hits"`
	CacheMisses      uint64  `json:"cache_misses"`
	ArchiveDownloads uint64  `json:"archive_downloads"`
	ArchiveBytes     uint64  `json:"archive_bytes"`
	ServeBytesPerSec float64 `json:"serve_bytes_per_sec"`
	AvgBytesPerSec   float64 `json:"avg_bytes_per_sec"`
	CacheEntries     int     `json:"cache_entries"`
	CacheBytes       int64   `json:"cache_bytes"`
	CacheMaxBytes    int64   `json:"cache_max_bytes"`
	VersionsStored   int     `json:"versions_stored"`
}

func New() *Collector {
	return &Collector{
		started: time.Now(),
		period:  newPeriodTracker(),
	}
}

func (c *Collector) EnableFileStore(dataDir string) error {
	fs, err := NewFileStore(dataDir)
	if err != nil {
		return err
	}
	c.files = fs
	return nil
}

func (c *Collector) FileStore() *FileStore {
	return c.files
}

func (c *Collector) RecordServe(bytes int64, cacheHit bool) {
	if bytes < 0 {
		bytes = 0
	}
	c.totalRequests.Add(1)
	c.totalBytes.Add(uint64(bytes))
	if cacheHit {
		c.cacheHits.Add(1)
	} else {
		c.cacheMisses.Add(1)
	}
	c.recordRecent(uint64(bytes))
}

func (c *Collector) RecordRequest(bytesIn int64, client string) {
	if bytesIn > 0 {
		c.totalBytesIn.Add(uint64(bytesIn))
	}
	c.period.recordRequest(bytesIn, client)
}

func (c *Collector) RecordBlocked() {
	c.blockedTotal.Add(1)
	c.period.recordBlocked()
}

func (c *Collector) RecordError() {
	c.errorsTotal.Add(1)
	c.period.recordError()
}

func (c *Collector) TotalBytesOut() uint64 {
	return c.totalBytes.Load()
}

func (c *Collector) FlushPeriod(bytesOut uint64) PeriodSnapshot {
	return c.period.flush(bytesOut)
}

func (c *Collector) RecordArchiveDownload(bytes int64) {
	if bytes < 0 {
		bytes = 0
	}
	c.archiveFiles.Add(1)
	c.archiveBytes.Add(uint64(bytes))
}

func (c *Collector) recordRecent(bytes uint64) {
	slot := (c.recentIdx.Add(1) - 1) % constants.StatsRingSize
	c.recentBytes[slot].Store(bytes)
	c.recentAt[slot].Store(time.Now().UnixNano())
}

func (c *Collector) recentRate() float64 {
	cutoff := time.Now().Add(-constants.StatsWindow).UnixNano()
	var total uint64
	for i := range c.recentAt {
		if c.recentAt[i].Load() >= cutoff {
			total += c.recentBytes[i].Load()
		}
	}
	return float64(total) / constants.StatsWindow.Seconds()
}

func (c *Collector) Snapshot(cacheEntries int, cacheBytes, cacheMaxBytes int64, versionsStored int) Snapshot {
	uptime := time.Since(c.started).Seconds()
	totalBytes := c.totalBytes.Load()
	avg := 0.0
	if uptime > 0 {
		avg = float64(totalBytes) / uptime
	}
	return Snapshot{
		UptimeSec:        uptime,
		TotalRequests:    c.totalRequests.Load(),
		TotalBytesServed: totalBytes,
		TotalBytesIn:     c.totalBytesIn.Load(),
		BlockedRequests:  c.blockedTotal.Load(),
		Errors:           c.errorsTotal.Load(),
		CacheHits:        c.cacheHits.Load(),
		CacheMisses:      c.cacheMisses.Load(),
		ArchiveDownloads: c.archiveFiles.Load(),
		ArchiveBytes:     c.archiveBytes.Load(),
		ServeBytesPerSec: c.recentRate(),
		AvgBytesPerSec:   avg,
		CacheEntries:     cacheEntries,
		CacheBytes:       cacheBytes,
		CacheMaxBytes:    cacheMaxBytes,
		VersionsStored:   versionsStored,
	}
}

func (c *Collector) WriteJSON(w interface{ Write([]byte) (int, error) }, cacheEntries int, cacheBytes, cacheMaxBytes int64, versionsStored int) error {
	snap := c.Snapshot(cacheEntries, cacheBytes, cacheMaxBytes, versionsStored)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
