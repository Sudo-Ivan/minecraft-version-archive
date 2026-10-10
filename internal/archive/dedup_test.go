package archive

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/stats"
)

func TestArchiveReusesHashIndex(t *testing.T) {
	content := []byte("shared-jar-content")
	sum := sha1.Sum(content)
	sha1hex := hex.EncodeToString(sum[:])

	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mc/game/version_manifest_v2.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"latest": map[string]string{"release": "ver-a", "snapshot": "ver-a"},
			"versions": []map[string]string{
				{"id": "ver-a", "type": "release", "url": baseURL + "/files/ver-a.json"},
				{"id": "ver-b", "type": "release", "url": baseURL + "/files/ver-b.json"},
			},
		})
	})
	for _, id := range []string{"ver-a", "ver-b"} {
		mux.HandleFunc("GET /files/"+id+".json", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"id": id,
				"downloads": map[string]any{
					"client": map[string]any{
						"sha1": sha1hex, "size": len(content), "url": baseURL + "/files/client.jar",
					},
				},
				"assetIndex": map[string]any{"id": "idx", "url": baseURL + "/files/index.json"},
				"libraries":  []any{},
			})
		})
	}
	downloads := 0
	mux.HandleFunc("GET /files/client.jar", func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.Write(content)
	})
	mux.HandleFunc("GET /files/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"objects":{}}`))
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()
	baseURL = ts.URL

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	ini := filepath.Join(dir, "settings.ini")
	if err := os.WriteFile(ini, []byte(`
[general]
data_dir = `+dataDir+`
public_url = http://mirror.test
[archive]
versions = ver-a,ver-b
client_only = true
workers = 1
retry_max = 2
retry_base_ms = 10
`), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := constants.MojangManifest
	constants.MojangManifest = ts.URL + "/mc/game/version_manifest_v2.json"
	t.Cleanup(func() { constants.MojangManifest = orig })

	cfg, err := config.Load(ini)
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	archiver := NewWithStats(cfg, log, stats.New())
	if err := archiver.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if downloads != 1 {
		t.Fatalf("client jar downloaded %d times, want 1", downloads)
	}
	layout := archiver.layout
	for _, id := range []string{"ver-a", "ver-b"} {
		if !layout.Exists(layout.ClientJar(id)) {
			t.Fatalf("missing jar for %s", id)
		}
	}
}
