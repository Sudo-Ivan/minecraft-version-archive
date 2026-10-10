package temurin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"minecraft-version-archive/internal/fetch"
)

var ErrUnavailable = errors.New("temurin release unavailable")

type UnavailableError struct {
	Major      int
	PlatformID string
	Err        error
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("temurin unavailable for java %d on %s: %v", e.Major, e.PlatformID, e.Err)
}

func (e *UnavailableError) Is(target error) bool {
	return target == ErrUnavailable
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func unavailable(major int, plat Platform, err error) error {
	return &UnavailableError{Major: major, PlatformID: plat.ID, Err: err}
}

const APIBase = "https://api.adoptium.net/v3"

var apiBase = APIBase

func TestAPIBase(base string) func() {
	old := apiBase
	apiBase = base
	return func() { apiBase = old }
}

type VersionData struct {
	Major          int    `json:"major"`
	Minor          int    `json:"minor"`
	Security       int    `json:"security"`
	Build          int    `json:"build"`
	OpenJDKVersion string `json:"openjdk_version"`
	Semver         string `json:"semver"`
}

type PackageInfo struct {
	Name     string `json:"name"`
	Link     string `json:"link"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
}

type Binary struct {
	OS        string      `json:"os"`
	Arch      string      `json:"architecture"`
	ImageType string      `json:"image_type"`
	Package   PackageInfo `json:"package"`
	UpdatedAt string      `json:"updated_at"`
}

type Release struct {
	ReleaseName string      `json:"release_name"`
	Timestamp   string      `json:"timestamp"`
	Vendor      string      `json:"vendor"`
	VersionData VersionData `json:"version_data"`
	Binaries    []Binary    `json:"binaries"`
}

func AssetsURL(major int, plat Platform) string {
	q := url.Values{}
	q.Set("architecture", plat.Arch)
	q.Set("image_type", "jdk")
	q.Set("os", plat.OS)
	q.Set("page_size", "1")
	return fmt.Sprintf("%s/assets/feature_releases/%d/ga?%s", apiBase, major, q.Encode())
}

func FetchRelease(ctx context.Context, client *fetch.Client, major int, plat Platform) (*Release, *Binary, error) {
	data, err := client.Get(ctx, AssetsURL(major, plat))
	if err != nil {
		if isHTTPNotFound(err) {
			return nil, nil, unavailable(major, plat, err)
		}
		return nil, nil, err
	}
	var releases []Release
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, nil, err
	}
	if len(releases) == 0 || len(releases[0].Binaries) == 0 {
		return nil, nil, unavailable(major, plat, fmt.Errorf("empty release list"))
	}
	rel := &releases[0]
	bin := pickBinary(rel.Binaries, plat)
	if bin == nil {
		return nil, nil, unavailable(major, plat, fmt.Errorf("no matching jdk binary"))
	}
	if bin.Package.Link == "" || bin.Package.Checksum == "" {
		return nil, nil, fmt.Errorf("incomplete temurin metadata for java %d on %s", major, plat.ID)
	}
	return rel, bin, nil
}

func isHTTPNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, ": 404") || strings.HasSuffix(msg, " 404 Not Found")
}

func pickBinary(binaries []Binary, plat Platform) *Binary {
	for i := range binaries {
		b := &binaries[i]
		if strings.EqualFold(b.OS, plat.OS) && strings.EqualFold(b.Arch, plat.Arch) && b.ImageType == "jdk" {
			return b
		}
	}
	return nil
}

func PackageType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	default:
		return ""
	}
}

func ReleaseTime(ts string) string {
	if ts == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format(time.RFC3339)
}
