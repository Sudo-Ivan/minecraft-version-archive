package fabric

import (
	"path/filepath"
	"strings"
)

func MetaStorageRel(apiRel string) string {
	apiRel = strings.TrimPrefix(filepath.ToSlash(apiRel), "/")
	parts := strings.Split(apiRel, "/")
	encodeMetaVersionParts(parts)
	if len(parts) < 3 || parts[0] != "v2" || parts[1] != "versions" {
		return strings.Join(parts, "/")
	}

	switch parts[2] {
	case "loader":
		switch len(parts) {
		case 3:
			return strings.Join(parts, "/") + ".json"
		case 4:
			return "v2/versions/loader/_mc/" + parts[3] + ".json"
		case 5:
			return "v2/versions/loader/_mc/" + parts[3] + "/_loader/" + parts[4] + ".json"
		}
		if len(parts) >= 7 {
			mc, loader := parts[3], parts[4]
			switch {
			case parts[len(parts)-2] == "profile" && parts[len(parts)-1] == "json":
				return "v2/versions/loader/_mc/" + mc + "/_loader/" + loader + "/profile.json"
			case parts[len(parts)-2] == "server" && parts[len(parts)-1] == "json":
				return "v2/versions/loader/_mc/" + mc + "/_loader/" + loader + "/server.json"
			}
		}
	case "intermediary":
		switch len(parts) {
		case 3:
			return strings.Join(parts, "/") + ".json"
		case 4:
			return "v2/versions/intermediary/_mc/" + parts[3] + ".json"
		}
	case "game":
		if len(parts) == 3 {
			return strings.Join(parts, "/") + ".json"
		}
	}

	return strings.Join(parts, "/")
}

func MetaRequestRel(requestPath string) string {
	return "v2/" + strings.TrimPrefix(filepath.ToSlash(requestPath), "/")
}

func MetaResolveStorage(requestPath string) string {
	return MetaStorageRel(MetaRequestRel(requestPath))
}
