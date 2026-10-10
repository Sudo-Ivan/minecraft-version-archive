package polymc

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

const fabricLoaderReleaseIndex = "https://meta.polymc.org/v1/net.fabricmc.fabric-loader/index.json"

var (
	releaseTimesMu      sync.Mutex
	releaseTimesCache   map[string]string
	releaseTimesExpiry  time.Time
	releaseTimesTTL     = time.Hour
	releaseTimesFetcher func(context.Context, string) ([]byte, error)
)

func SetReleaseTimesFetcher(fetch func(context.Context, string) ([]byte, error)) {
	releaseTimesMu.Lock()
	releaseTimesFetcher = fetch
	releaseTimesMu.Unlock()
}

// LoaderReleaseTimes returns cached loader version release times from meta.polymc.org.
func LoaderReleaseTimes(ctx context.Context) map[string]string {
	releaseTimesMu.Lock()
	fetch := releaseTimesFetcher
	if releaseTimesCache != nil && time.Now().Before(releaseTimesExpiry) {
		out := releaseTimesCache
		releaseTimesMu.Unlock()
		return out
	}
	releaseTimesMu.Unlock()

	if fetch == nil {
		return nil
	}
	data, err := fetch(ctx, fabricLoaderReleaseIndex)
	if err != nil {
		return nil
	}
	var list VersionList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil
	}
	out := make(map[string]string, len(list.Versions))
	for _, v := range list.Versions {
		if v.Version != "" && v.ReleaseTime != "" {
			out[v.Version] = v.ReleaseTime
		}
	}

	releaseTimesMu.Lock()
	releaseTimesCache = out
	releaseTimesExpiry = time.Now().Add(releaseTimesTTL)
	releaseTimesMu.Unlock()
	return out
}

func releaseTimeFor(version string, times map[string]string) string {
	if t, ok := times[version]; ok {
		return t
	}
	return ""
}

func applyReleaseTimes(entries []VersionEntry, times map[string]string) {
	if len(times) == 0 {
		return
	}
	for i := range entries {
		if t := releaseTimeFor(entries[i].Version, times); t != "" {
			entries[i].ReleaseTime = t
		}
	}
}
