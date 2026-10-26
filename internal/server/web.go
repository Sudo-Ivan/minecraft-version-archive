package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"minecraft-version-archive/internal/constants"
)

//go:embed web
var webFS embed.FS

// webAsset is a fully prepared response body for an embedded site file:
// identity and gzip bodies, content type, cache policy and ETag, computed
// once at init so requests stay allocation-free.
type webAsset struct {
	body []byte
	gz   []byte
	ct   []string
	etag string
	cc   string
}

var (
	webIndex  webAsset
	webDocs   webAsset
	webAssets map[string]webAsset
)

var webContentTypes = map[string][]string{
	".css":   {constants.MIMECSS},
	".js":    {constants.MIMECSSJS},
	".woff2": {constants.MIMEWOFF2},
}

func init() {
	webIndex = loadWebPage("web/index.html")
	webDocs = loadWebPage("web/docs.html")
	webAssets = make(map[string]webAsset)
	err := fs.WalkDir(webFS, "web/assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ct, ok := webContentTypes[path.Ext(p)]
		if !ok {
			return nil
		}
		rel := strings.TrimPrefix(p, "web/assets/")
		webAssets[rel] = loadWebAsset(p, ct)
		return nil
	})
	if err != nil {
		panic(err)
	}
}

func loadWebPage(name string) webAsset {
	return loadWebAsset(name, constants.ContentTypeHTML)
}

func loadWebAsset(name string, ct []string) webAsset {
	body, err := webFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	a := webAsset{body: body, ct: ct, etag: makeETag(body), cc: constants.CacheControlShort}
	if path.Ext(name) == ".woff2" {
		a.cc = constants.CacheControlImmutable
	}
	if len(body) >= constants.GzipMinBytes {
		a.gz = gzipData(body)
	}
	return a
}

// acceptsHTML reports whether the client prefers an HTML page over the JSON
// index: browsers send text/html in Accept, launchers and tools do not.
func acceptsHTML(r *http.Request) bool {
	for tok := range strings.SplitSeq(r.Header.Get(constants.HeaderAccept), ",") {
		if strings.TrimSpace(strings.SplitN(tok, ";", 2)[0]) == "text/html" {
			return true
		}
	}
	return false
}

func (s *Server) serveWebAsset(w http.ResponseWriter, r *http.Request, a webAsset) {
	w.Header()[constants.HeaderContentType] = a.ct
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	if a.ct[0] == constants.ContentTypeHTML[0] {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; font-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	}
	s.emitTagged(w, r, a.body, a.gz, false, a.cc, a.etag)
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	s.serveWebAsset(w, r, webDocs)
}

func (s *Server) handleWebAsset(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("file")
	a, ok := webAssets[rel]
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.serveWebAsset(w, r, a)
}
