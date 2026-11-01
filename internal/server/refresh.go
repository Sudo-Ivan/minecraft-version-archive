package server

import (
	"context"
	"errors"
	"time"

	"minecraft-version-archive/internal/archive"
)

// StartAutoRefresh launches a background loop that re-runs the archiver every
// cfg.RefreshMin minutes so new Mojang releases are picked up without manual
// intervention. An immediate run happens on startup; later ticks are skipped
// while a run is still in progress.
func (s *Server) StartAutoRefresh(ctx context.Context) {
	interval := time.Duration(s.cfg.RefreshMin) * time.Minute
	go func() {
		s.log.Info("auto-refresh enabled", "interval", interval)
		s.refreshArchive(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.refreshArchive(ctx)
			}
		}
	}()
}

func (s *Server) refreshArchive(ctx context.Context) {
	err := s.archiver.TryRun(ctx)
	switch {
	case err == nil:
		s.invExpiry.Store(0)
	case errors.Is(err, archive.ErrInProgress):
		s.log.Debug("auto-refresh skipped, archive in progress")
	default:
		s.log.Warn("auto-refresh failed", "err", err)
	}
}
