package fabric

import (
	"bytes"
	"strings"
)

func RewriteMeta(data []byte, publicURL string) []byte {
	if publicURL == "" {
		return data
	}
	base := strings.TrimRight(publicURL, "/")
	repl := []byte(base + "/maven/fabricmc/")
	return bytes.ReplaceAll(data, []byte("https://maven.fabricmc.net/"), repl)
}
