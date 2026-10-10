package config

import "testing"

func TestApplyArchiveCLI(t *testing.T) {
	cfg := &Config{Versions: []string{"9.9"}, CacheBytes: 1}
	if err := cfg.ApplyArchiveCLI(ArchiveCLIInput{Versions: []string{"1.5.0"}}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Versions) != 1 || cfg.Versions[0] != "1.5.0" {
		t.Fatalf("Versions = %v", cfg.Versions)
	}
	if cfg.VersionsFile != "" {
		t.Fatalf("VersionsFile = %q", cfg.VersionsFile)
	}
}

func TestApplyArchiveCLINoOverride(t *testing.T) {
	cfg := &Config{Versions: []string{"1.20.1"}, CacheBytes: 1}
	if err := cfg.ApplyArchiveCLI(ArchiveCLIInput{}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Versions) != 1 || cfg.Versions[0] != "1.20.1" {
		t.Fatalf("Versions = %v", cfg.Versions)
	}
}

func TestApplyArchiveCLIAllConflict(t *testing.T) {
	cfg := &Config{CacheBytes: 1}
	if err := cfg.ApplyArchiveCLI(ArchiveCLIInput{All: true, Versions: []string{"1.5.0"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyArchiveCLIFileConflict(t *testing.T) {
	cfg := &Config{CacheBytes: 1}
	if err := cfg.ApplyArchiveCLI(ArchiveCLIInput{VersionsFile: "versions.txt", Versions: []string{"1.5.0"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyArchiveCLIAllReleasesOnly(t *testing.T) {
	cfg := &Config{Versions: []string{"1.5.0"}, CacheBytes: 1}
	if err := cfg.ApplyArchiveCLI(ArchiveCLIInput{All: true}); err != nil {
		t.Fatal(err)
	}
	if !cfg.ArchiveAll {
		t.Fatal("ArchiveAll should be true")
	}
	if cfg.IncludeSnapshots || cfg.IncludePreReleases || cfg.IncludeRC {
		t.Fatal("optional includes should stay false")
	}
	if len(cfg.Versions) != 0 {
		t.Fatalf("Versions = %v", cfg.Versions)
	}
}

func TestApplyArchiveCLIIncludeAllTypes(t *testing.T) {
	cfg := &Config{CacheBytes: 1}
	if err := cfg.ApplyArchiveCLI(ArchiveCLIInput{All: true, IncludeAll: true}); err != nil {
		t.Fatal(err)
	}
	if !cfg.ArchiveAll || !cfg.IncludesAllOptionalTypes() {
		t.Fatal("expected all optional types enabled")
	}
}

func TestAllowsVersionChannel(t *testing.T) {
	cfg := &Config{IncludeSnapshots: true, IncludeRC: true, CacheBytes: 1}
	if !cfg.AllowsVersionChannel("release") {
		t.Fatal("releases should always be allowed")
	}
	if !cfg.AllowsVersionChannel("snapshot") {
		t.Fatal("snapshots should be allowed")
	}
	if cfg.AllowsVersionChannel("prerelease") {
		t.Fatal("prereleases should be blocked")
	}
	if !cfg.AllowsVersionChannel("rc") {
		t.Fatal("rc should be allowed")
	}
}
