package temurin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"minecraft-version-archive/internal/fetch"
)

func TestAssetsURL(t *testing.T) {
	plat := DefaultPlatforms[0]
	got := AssetsURL(21, plat)
	if !strings.Contains(got, "/assets/feature_releases/21/ga") {
		t.Fatalf("url = %q", got)
	}
	if !strings.Contains(got, "architecture=x64") || !strings.Contains(got, "os=linux") {
		t.Fatalf("url = %q", got)
	}
}

func TestFetchRelease(t *testing.T) {
	binBody := []byte("jdk-binary")
	checksum := "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"
	var dlURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/assets/"):
			payload := []Release{{
				ReleaseName: "jdk-21.0.11+10",
				Timestamp:   "2026-04-23T06:32:33Z",
				VersionData: VersionData{Major: 21, Minor: 0, Security: 11, Build: 10, OpenJDKVersion: "21.0.11+10-LTS"},
				Binaries: []Binary{{
					OS:        "linux",
					Arch:      "x64",
					ImageType: "jdk",
					Package: PackageInfo{
						Name:     "OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz",
						Link:     dlURL,
						Checksum: checksum,
						Size:     int64(len(binBody)),
					},
				}},
			}}
			_ = json.NewEncoder(w).Encode(payload)
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			w.Write(binBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dlURL = srv.URL + "/OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz"

	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })

	client := fetch.New(2, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rel, bin, err := FetchRelease(ctx, client, 21, DefaultPlatforms[0])
	if err != nil {
		t.Fatal(err)
	}
	if rel.ReleaseName != "jdk-21.0.11+10" {
		t.Fatalf("release = %q", rel.ReleaseName)
	}
	if bin.Package.Name == "" || bin.Package.Checksum != checksum {
		t.Fatalf("bin = %+v", bin.Package)
	}
}

func TestFetchReleaseMissingBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"release_name":"x","binaries":[{"os":"linux","architecture":"arm","image_type":"jdk","package":{"name":"x","link":"http://x","checksum":"y"}}]}]`))
	}))
	defer srv.Close()

	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })

	_, _, err := FetchRelease(context.Background(), fetch.New(1, 10), 21, DefaultPlatforms[0])
	if !IsUnavailable(err) {
		t.Fatalf("expected unavailable, got %v", err)
	}
}

func TestFetchReleaseHTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Cleanup(TestAPIBase(srv.URL))

	_, _, err := FetchRelease(context.Background(), fetch.New(1, 10), 8, DefaultPlatforms[4])
	if !IsUnavailable(err) {
		t.Fatalf("expected unavailable, got %v", err)
	}
}

func TestPickBinary(t *testing.T) {
	bins := []Binary{
		{OS: "linux", Arch: "aarch64", ImageType: "jdk"},
		{OS: "linux", Arch: "x64", ImageType: "jdk"},
	}
	got := pickBinary(bins, DefaultPlatforms[0])
	if got == nil || got.Arch != "x64" {
		t.Fatalf("got %+v", got)
	}
}

func TestReleaseTime(t *testing.T) {
	got := ReleaseTime("2026-04-23T06:32:33Z")
	if got != "2026-04-23T06:32:33Z" {
		t.Fatalf("got %q", got)
	}
	if ReleaseTime("not-a-time") != "not-a-time" {
		t.Fatal("expected passthrough")
	}
}
