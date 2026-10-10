package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/stats"
)

func TestBlockScannerPaths(t *testing.T) {
	blocked := []string{
		"/.env",
		"/.git/config",
		"/wp-admin/setup.php",
		"/api/.env",
		"/vendor/phpunit/phpunit/src/Util/PHP/eval-stdin.php",
		"/actuator/health",
		"/phpmyadmin/index.php",
	}
	for _, path := range blocked {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if !isScannerRequest(r) {
			t.Fatalf("expected %q to be blocked", path)
		}
	}
}

func TestAllowMinecraftPaths(t *testing.T) {
	allowed := []string{
		"/health",
		"/mc/game/version_manifest_v2.json",
		"/versions/1.21.1.json",
		"/jars/client/1.21.1.jar",
		"/libraries/com/mojang/authlib/6.0.57/authlib-6.0.57.jar",
		"/assets/objects/ab/abcdef0123456789",
		"/fabric/v2/versions/loader",
		"/p2p/inventory",
		"/java/temurin",
		"/java/temurin/21/linux-x64/jdk.tar.gz",
	}
	for _, path := range allowed {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if isScannerRequest(r) {
			t.Fatalf("expected %q to be allowed", path)
		}
	}
}

func TestBlockNonGetMethods(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/versions/1.0.json", nil)
	if !isScannerRequest(r) {
		t.Fatal("expected POST to non-p2p path to be blocked")
	}
}

func TestBlockMiddleware(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:       dir,
		PublicURL:     "http://test",
		BlockScanners: true,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	archiver := archive.NewWithStats(cfg, log, stats.New())
	srv := New(cfg, archiver, log)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.env", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
