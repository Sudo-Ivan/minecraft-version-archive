package config

import (
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/constants"
)

func TestLoadNoConfigFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != filepath.Clean(constants.DefaultDataDir) {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.Listen != constants.DefaultListen {
		t.Fatalf("Listen = %q", cfg.Listen)
	}
	if cfg.CacheBytes != int64(constants.DefaultCacheMB*1024*1024) {
		t.Fatalf("CacheBytes = %d", cfg.CacheBytes)
	}
	if cfg.PublicURL != "http://localhost:8080" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
}

func TestLoadEmptyPathUsesDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CacheEnabled {
		t.Fatal("cache should be enabled by default")
	}
	if cfg.CacheShards != constants.DefaultCacheShards {
		t.Fatalf("CacheShards = %d", cfg.CacheShards)
	}
}

func TestDerivePublicURL(t *testing.T) {
	cases := map[string]string{
		":8080":        "http://localhost:8080",
		"0.0.0.0:9000": "http://localhost:9000",
		"[::]:8080":    "http://localhost:8080",
		"127.0.0.1:80": "http://127.0.0.1:80",
	}
	for listen, want := range cases {
		if got := derivePublicURL(listen); got != want {
			t.Fatalf("derivePublicURL(%q) = %q, want %q", listen, got, want)
		}
	}
}

func TestApplyServeOverrides(t *testing.T) {
	cases := []struct {
		name       string
		baseListen string
		host       string
		port       int
		positional string
		wantListen string
		wantPublic string
	}{
		{"port flag", ":8080", "", 8070, "", ":8070", "http://localhost:8070"},
		{"positional port", ":8080", "", 0, "8077", ":8077", "http://localhost:8077"},
		{"host flag", ":8080", "127.0.0.1", 0, "", "127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"host and port flags", ":8080", "0.0.0.0", 9000, "", "0.0.0.0:9000", "http://localhost:9000"},
		{"positional host port", ":8080", "", 0, "127.0.0.1:8070", "127.0.0.1:8070", "http://127.0.0.1:8070"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Listen: tc.baseListen, PublicURL: derivePublicURL(tc.baseListen), CacheBytes: 1}
			if err := cfg.ApplyServeOverrides(tc.host, tc.port, tc.positional, ""); err != nil {
				t.Fatal(err)
			}
			if cfg.Listen != tc.wantListen {
				t.Fatalf("Listen = %q, want %q", cfg.Listen, tc.wantListen)
			}
			if cfg.PublicURL != tc.wantPublic {
				t.Fatalf("PublicURL = %q, want %q", cfg.PublicURL, tc.wantPublic)
			}
		})
	}
}

func TestApplyServeOverridesInvalidPort(t *testing.T) {
	cfg := &Config{Listen: ":8080", CacheBytes: 1}
	if err := cfg.ApplyServeOverrides("", 0, "nope", ""); err == nil {
		t.Fatal("expected error for invalid positional port")
	}
	if err := cfg.ApplyServeOverrides("", 70000, "", ""); err == nil {
		t.Fatal("expected error for invalid port flag")
	}
}

func TestHasExplicitArchiveSelection(t *testing.T) {
	def := Default()
	if def.HasExplicitArchiveSelection() {
		t.Fatal("defaults should not be explicit archive selection")
	}
	explicit := &Config{Versions: []string{"1.20.1"}, CacheBytes: 1}
	if !explicit.HasExplicitArchiveSelection() {
		t.Fatal("versions should be explicit")
	}
}

func TestLoadCacheMB(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "settings.ini")
	if err := os.WriteFile(ini, []byte(`
[cache]
max_mb = 128
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(ini)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(128 * 1024 * 1024)
	if cfg.CacheBytes != want {
		t.Fatalf("CacheBytes = %d, want %d", cfg.CacheBytes, want)
	}
}

func TestLoadCacheGB(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "settings.ini")
	if err := os.WriteFile(ini, []byte(`
[cache]
max_gb = 2
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(ini)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(2 * 1024 * 1024 * 1024)
	if cfg.CacheBytes != want {
		t.Fatalf("CacheBytes = %d, want %d", cfg.CacheBytes, want)
	}
}

func TestLoadCacheGBPriorityOverMB(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "settings.ini")
	if err := os.WriteFile(ini, []byte(`
[cache]
max_gb = 1
max_mb = 512
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(ini)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(1024 * 1024 * 1024)
	if cfg.CacheBytes != want {
		t.Fatalf("CacheBytes = %d, want %d", cfg.CacheBytes, want)
	}
}

func TestEnvOverridesINI(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "settings.ini")
	if err := os.WriteFile(ini, []byte(`
[general]
data_dir = ./old
[cache]
max_mb = 64
[stats]
enabled = false
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(constants.EnvPrefix+"DATA_DIR", "/tmp/mva-data")
	t.Setenv(constants.EnvPrefix+"CACHE_MB", "256")
	t.Setenv(constants.EnvPrefix+"STATS_ENABLED", "true")
	t.Setenv(constants.EnvPrefix+"VERSIONS", "1.20.1,1.19.4")

	cfg, err := Load(ini)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/tmp/mva-data" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.CacheBytes != 256*1024*1024 {
		t.Fatalf("CacheBytes = %d", cfg.CacheBytes)
	}
	if !cfg.StatsEnabled {
		t.Fatal("StatsEnabled should be true")
	}
	if len(cfg.Versions) != 2 {
		t.Fatalf("Versions = %v", cfg.Versions)
	}
}

func TestResolveVersionIDsFromFile(t *testing.T) {
	dir := t.TempDir()
	versions := filepath.Join(dir, "versions.txt")
	if err := os.WriteFile(versions, []byte("1.20.1\n# comment\n1.19.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{VersionsFile: versions}
	ids, err := cfg.ResolveVersionIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "1.20.1" || ids[1] != "1.19.4" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestValidateClientAndServerOnly(t *testing.T) {
	cfg := &Config{ClientOnly: true, ServerOnly: true, CacheBytes: 1}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseSizeValue(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"512", 512},
		{"512mb", 512},
		{"1gb", 1},
		{" 2 g ", 2},
	}
	for _, tc := range cases {
		got, err := parseSizeValue(tc.in)
		if err != nil {
			t.Fatalf("parseSizeValue(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseSizeValue(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
