package fabric

import (
	"strings"
)

func MavenRelPath(coord string) string {
	parts := strings.Split(coord, ":")
	if len(parts) < 3 {
		return ""
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact := parts[1]
	version := parts[2]
	classifier := ""
	ext := "jar"
	switch len(parts) {
	case 4:
		if parts[3] == "json" || parts[3] == "zip" {
			ext = parts[3]
		} else {
			classifier = "-" + parts[3]
		}
	case 5:
		classifier = "-" + parts[3]
		ext = parts[4]
	}
	name := artifact + "-" + version + classifier + "." + ext
	return group + "/" + artifact + "/" + version + "/" + name
}

func MavenDownloadURL(base, coord string) string {
	rel := MavenRelPath(coord)
	if rel == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/" + rel
}
