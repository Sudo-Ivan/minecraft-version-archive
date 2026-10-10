package temurin

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"minecraft-version-archive/internal/store"
)

const Vendor = "Eclipse Temurin"

type Checksum struct {
	Type string `json:"type"`
	Hash string `json:"hash"`
}

type VersionInfo struct {
	Name     string `json:"name"`
	Major    int    `json:"major"`
	Minor    int    `json:"minor"`
	Security int    `json:"security"`
	Build    int    `json:"build"`
}

type PlatformEntry struct {
	RuntimeOS    string   `json:"runtimeOS"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	File         string   `json:"file"`
	URL          string   `json:"url"`
	PackageType  string   `json:"packageType"`
	DownloadType string   `json:"downloadType"`
	Size         int64    `json:"size"`
	Checksum     Checksum `json:"checksum"`
}

type MajorManifest struct {
	Vendor      string          `json:"vendor"`
	Major       int             `json:"major"`
	ReleaseName string          `json:"release_name"`
	ReleaseTime string          `json:"release_time"`
	Version     VersionInfo     `json:"version"`
	Platforms   []PlatformEntry `json:"platforms"`
}

type GlobalIndex struct {
	Majors []int `json:"majors"`
}

func MajorManifestPath(layout *store.Layout, major int) string {
	return filepath.Join(layout.JavaTemurinDir(), fmt.Sprintf("%d", major), "manifest.json")
}

func PlatformFilePath(layout *store.Layout, major int, platformID, file string) string {
	return filepath.Join(layout.JavaTemurinDir(), fmt.Sprintf("%d", major), platformID, file)
}

func GlobalIndexPath(layout *store.Layout) string {
	return filepath.Join(layout.JavaTemurinDir(), "index.json")
}

func ReadMajorManifest(layout *store.Layout, major int) (*MajorManifest, bool) {
	data, err := layout.ReadFile(MajorManifestPath(layout, major))
	if err != nil {
		return nil, false
	}
	var man MajorManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, false
	}
	return &man, true
}

func ReadGlobalIndex(layout *store.Layout) (*GlobalIndex, error) {
	data, err := layout.ReadFile(GlobalIndexPath(layout))
	if err != nil {
		return &GlobalIndex{}, nil
	}
	var idx GlobalIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func WriteGlobalIndex(layout *store.Layout, majors []int) error {
	return store.WriteJSON(layout, GlobalIndexPath(layout), GlobalIndex{Majors: majors})
}

func BuildMajorManifest(major int, rel *Release, entries []PlatformEntry) MajorManifest {
	v := rel.VersionData
	name := v.OpenJDKVersion
	if name == "" {
		name = rel.ReleaseName
	}
	return MajorManifest{
		Vendor:      Vendor,
		Major:       major,
		ReleaseName: rel.ReleaseName,
		ReleaseTime: ReleaseTime(rel.Timestamp),
		Version: VersionInfo{
			Name:     name,
			Major:    v.Major,
			Minor:    v.Minor,
			Security: v.Security,
			Build:    v.Build,
		},
		Platforms: entries,
	}
}

func EntryFromBinary(publicURL string, major int, plat Platform, rel *Release, bin *Binary) PlatformEntry {
	base := strings.TrimRight(publicURL, "/")
	file := bin.Package.Name
	return PlatformEntry{
		RuntimeOS:    plat.RuntimeOS,
		OS:           plat.OS,
		Arch:         plat.Arch,
		File:         file,
		URL:          fmt.Sprintf("%s/java/temurin/%d/%s/%s", base, major, plat.ID, file),
		PackageType:  PackageType(file),
		DownloadType: "Archive",
		Size:         bin.Package.Size,
		Checksum: Checksum{
			Type: "sha-256",
			Hash: bin.Package.Checksum,
		},
	}
}

func FindPlatformEntry(man *MajorManifest, platformID, file string) (*PlatformEntry, bool) {
	for i := range man.Platforms {
		e := &man.Platforms[i]
		if e.File != file {
			continue
		}
		if platformID == "" || platIDFromEntry(e) == platformID || e.RuntimeOS == platformID {
			return e, true
		}
	}
	return nil, false
}

func platIDFromEntry(e *PlatformEntry) string {
	for _, p := range DefaultPlatforms {
		if p.RuntimeOS == e.RuntimeOS || (p.OS == e.OS && p.Arch == e.Arch) {
			return p.ID
		}
	}
	return e.RuntimeOS
}

func AppendMCLink(dst []byte, major int) []byte {
	if len(dst) == 0 || dst[len(dst)-1] != '}' {
		return dst
	}
	dst = dst[:len(dst)-1]
	dst = append(dst, `,"temurin":"/java/temurin/`...)
	dst = appendInt(dst, major)
	dst = append(dst, `"}`...)
	return dst
}

func appendInt(dst []byte, n int) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	neg := n < 0
	if neg {
		n = -n
		dst = append(dst, '-')
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return append(dst, buf[i:]...)
}
