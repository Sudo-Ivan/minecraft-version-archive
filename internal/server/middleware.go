package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"minecraft-version-archive/internal/stats"
)

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	if s.debugLog {
		h = s.debugMiddleware(h)
	}
	h = s.statsMiddleware(h)
	if s.cfg.BlockScanners {
		h = s.blockScanners(h)
	}
	return h
}

func (s *Server) debugMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Debug("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
	})
}

func (s *Server) statsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.stats.RecordRequest(requestBytesIn(r), clientIP(r, s.cfg.TrustProxy))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) StartPeriodicStats(ctx context.Context, w io.Writer, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var prevOut uint64
		var lastWidth int
		emit := func() {
			curOut := s.stats.TotalBytesOut()
			delta := curOut - prevOut
			prevOut = curOut
			snap := s.stats.FlushPeriod(delta)
			line := stats.FormatPeriodLine(snap)
			width, _ := stats.WritePeriodLine(w, snap, stats.PeriodLineOpts{
				Refresh:   true,
				LastWidth: lastWidth,
			})
			lastWidth = width
			if fs := s.stats.FileStore(); fs != nil {
				_ = fs.AppendHistory(line, time.Now())
			}
		}
		emit()
		for {
			select {
			case <-ctx.Done():
				_, _ = io.WriteString(w, "\n")
				return
			case <-ticker.C:
				emit()
			}
		}
	}()
}

func requestBytesIn(r *http.Request) int64 {
	if r.ContentLength > 0 {
		return r.ContentLength
	}
	n := int64(len(r.Method) + len(r.URL.Path) + len(r.Proto) + 32)
	if host := r.Host; host != "" {
		n += int64(len(host))
	}
	return n
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if before, _, ok := strings.Cut(fwd, ","); ok {
				return strings.TrimSpace(before)
			}
			return strings.TrimSpace(fwd)
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
			return real
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
