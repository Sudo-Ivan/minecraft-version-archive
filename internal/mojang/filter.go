package mojang

import "strings"

func VersionChannel(id, typ string) string {
	switch typ {
	case "release":
		return "release"
	case "old_alpha":
		return "alpha"
	case "old_beta":
		return "beta"
	case "snapshot":
		if strings.Contains(id, "-rc-") {
			return "rc"
		}
		if strings.Contains(id, "-pre-") {
			return "prerelease"
		}
		return "snapshot"
	default:
		return "unknown"
	}
}

func FilterVersionIDs(versions []ManifestVersion, allow func(channel string) bool) []string {
	ids := make([]string, 0, len(versions))
	for _, v := range versions {
		if allow(VersionChannel(v.ID, v.Type)) {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

func CountByChannel(versions []ManifestVersion) map[string]int {
	counts := make(map[string]int)
	for _, v := range versions {
		counts[VersionChannel(v.ID, v.Type)]++
	}
	return counts
}
