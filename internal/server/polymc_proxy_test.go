package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

func TestPolyMCFabricLoaderIndexProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/versions/loader/26.2":
			w.Write([]byte(`[{"loader":{"version":"0.19.3","stable":true}}]`))
		case "/v2/versions/loader":
			w.Write([]byte(`[{"version":"0.19.3","stable":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	oldBase := constants.FabricMetaBase
	constants.FabricMetaBase = upstream.URL
	defer func() { constants.FabricMetaBase = oldBase }()

	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:      dir,
		PublicURL:    "http://test",
		CacheEnabled: false,
		ProxyOnMiss:  true,
		RetryMax:     3,
		RetryBaseMS:  10,
	}
	log := slog.New(slog.DiscardHandler)
	st := stats.New()
	srv := New(cfg, archive.NewWithStats(cfg, log, st), log)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/meta/v1/net.fabricmc.fabric-loader/index.json?minecraft=26.2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scoped status = %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Versions) != 1 || doc.Versions[0].Version != "0.19.3" {
		t.Fatalf("body = %s", got)
	}

	resp, err = http.Get(ts.URL + "/meta/v1/net.fabricmc.fabric-loader/index.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("global status = %d", resp.StatusCode)
	}
}

func TestPolyMCFabricLoaderIndexNoProxy(t *testing.T) {
	ts, _ := testServerLayout(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/meta/v1/net.fabricmc.fabric-loader/index.json?minecraft=26.2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
