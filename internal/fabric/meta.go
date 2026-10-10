package fabric

import (
	"net/url"
	"path/filepath"
	"strings"

	"minecraft-version-archive/internal/constants"
)

func MetaURL(path string) string {
	p := normalizeMetaAPIRel(strings.TrimPrefix(path, "/"))
	return strings.TrimRight(constants.FabricMetaBase, "/") + "/" + p
}

func normalizeMetaAPIRel(apiRel string) string {
	apiRel = strings.TrimPrefix(filepath.ToSlash(apiRel), "/")
	parts := strings.Split(apiRel, "/")
	encodeMetaVersionParts(parts)
	return strings.Join(parts, "/")
}

func encodeMetaVersionParts(parts []string) {
	if len(parts) < 3 || parts[0] != "v2" || parts[1] != "versions" {
		return
	}
	switch parts[2] {
	case "loader":
		if len(parts) >= 4 {
			parts[3] = encodeVersionSegment(parts[3])
		}
		if len(parts) >= 5 {
			parts[4] = encodeVersionSegment(parts[4])
		}
	case "intermediary":
		if len(parts) >= 4 {
			parts[3] = encodeVersionSegment(parts[3])
		}
	}
}

func encodeVersionSegment(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		s = u
	}
	return url.PathEscape(s)
}

func MavenURL(path string) string {
	return strings.TrimRight(constants.FabricMavenBase, "/") + "/" + strings.TrimPrefix(path, "/")
}

func EncodeGameVersion(id string) string {
	return encodeVersionSegment(id)
}

func MetaRelPath(path string) string {
	return strings.TrimPrefix(strings.TrimPrefix(path, "/"), "v2/")
}
