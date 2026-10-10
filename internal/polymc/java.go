package polymc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"minecraft-version-archive/internal/store"
	"minecraft-version-archive/internal/temurin"
)

// runtimeOSName maps our Temurin runtimeOS ids onto Prism meta runtimeOS
// naming (osx-* becomes mac-os-*).
func runtimeOSName(os string) string {
	switch os {
	case "osx-x64":
		return "mac-os-x64"
	case "osx-arm64":
		return "mac-os-arm64"
	}
	return os
}

// temurinPlatID resolves the directory platform id for a manifest entry,
// matching the ids used under java/temurin/{major}/ on disk.
func temurinPlatID(e *temurin.PlatformEntry) string {
	for _, p := range temurin.DefaultPlatforms {
		if p.RuntimeOS == e.RuntimeOS || (p.OS == e.OS && p.Arch == e.Arch) {
			return p.ID
		}
	}
	return e.RuntimeOS
}

// JavaIndex builds the net.minecraft.java component index from archived
// Temurin majors. Versions are named java{major} like upstream meta.
func JavaIndex(layout *store.Layout, publicURL string) ([]byte, error) {
	idx, err := temurin.ReadGlobalIndex(layout)
	if err != nil {
		return nil, err
	}
	majors := append([]int(nil), idx.Majors...)
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))
	entries := make([]VersionEntry, 0, len(majors))
	for _, major := range majors {
		man, ok := temurin.ReadMajorManifest(layout, major)
		if !ok {
			continue
		}
		body, err := javaRuntimeDetail(major, man, publicURL)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(body)
		entries = append(entries, VersionEntry{
			Version:     "java" + strconv.Itoa(major),
			Type:        "release",
			ReleaseTime: man.ReleaseTime,
			SHA256:      hex.EncodeToString(sum[:]),
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no java runtimes archived")
	}
	return marshalVersionList(UIDJava, "Java Runtimes", entries)
}

// JavaVersion emits a net.minecraft.java/{javaN}.json detail file backed by
// the archived Temurin manifest for that major.
func JavaVersion(layout *store.Layout, publicURL, version string) ([]byte, error) {
	if !strings.HasPrefix(version, "java") {
		return nil, fmt.Errorf("unknown java runtime %q", version)
	}
	major, err := strconv.Atoi(strings.TrimPrefix(version, "java"))
	if err != nil || major <= 0 {
		return nil, fmt.Errorf("unknown java runtime %q", version)
	}
	man, ok := temurin.ReadMajorManifest(layout, major)
	if !ok {
		return nil, fmt.Errorf("java runtime %s not archived", version)
	}
	return javaRuntimeDetail(major, man, publicURL)
}

func javaRuntimeDetail(major int, man *temurin.MajorManifest, publicURL string) ([]byte, error) {
	base := strings.TrimRight(publicURL, "/")
	runtimes := make([]any, 0, len(man.Platforms))
	for i := range man.Platforms {
		e := &man.Platforms[i]
		platID := temurinPlatID(e)
		name := "eclipse_temurin_" + strings.TrimPrefix(man.ReleaseName, "jdk-")
		rt := map[string]any{
			"checksum": map[string]any{
				"hash": e.Checksum.Hash,
				"type": "sha256",
			},
			"downloadType": "archive",
			"name":         name,
			"packageType":  "jdk",
			"releaseTime":  man.ReleaseTime,
			"runtimeOS":    runtimeOSName(e.RuntimeOS),
			"url":          fmt.Sprintf("%s/java/temurin/%d/%s/%s", base, major, platID, e.File),
			"vendor":       "eclipse",
			"version": map[string]any{
				"major":    man.Version.Major,
				"minor":    man.Version.Minor,
				"security": man.Version.Security,
				"build":    man.Version.Build,
				"name":     man.Version.Name,
			},
		}
		runtimes = append(runtimes, rt)
	}
	out := map[string]any{
		"formatVersion": 1,
		"name":          "Java " + strconv.Itoa(major),
		"releaseTime":   man.ReleaseTime,
		"runtimes":      runtimes,
	}
	return json.MarshalIndent(out, "", "    ")
}
