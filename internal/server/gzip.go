package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strings"

	"minecraft-version-archive/internal/constants"
)

// acceptsGzip reports whether the client offered gzip in Accept-Encoding.
func acceptsGzip(r *http.Request) bool {
	ae := r.Header.Get(constants.HeaderAcceptEncoding)
	for _, tok := range strings.Split(ae, ",") {
		if strings.TrimSpace(strings.SplitN(tok, ";", 2)[0]) == "gzip" {
			return true
		}
	}
	return false
}

// gzipData compresses data once so the gzipped body can be cached alongside
// the identity body in render and meta caches.
func gzipData(data []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

// gzipEligible reports whether a response body should be offered compressed:
// large enough to matter, and the client did not send a Range request (ranges
// are only served over the identity body).
func gzipEligible(r *http.Request, data []byte) bool {
	return len(data) >= constants.GzipMinBytes &&
		r.Header.Get(constants.HeaderRange) == "" &&
		acceptsGzip(r)
}
