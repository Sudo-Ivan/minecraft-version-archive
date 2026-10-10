package server

import (
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
)

type renderEntry struct {
	publicURL string
	size      int64
	modNano   int64
	body      []byte
	gz        []byte
	etag      string
}

type fileSig struct {
	size    int64
	modNano int64
}

// renderFile returns the rendered response for path, re-rendering only when
// the file's size or mtime changed or the public URL differs.
func (s *Server) renderFile(path, publicURL string, render func([]byte) ([]byte, error)) (*renderEntry, error) {
	info, err := s.layout.Stat(path)
	if err != nil {
		return nil, err
	}
	modNano := info.ModTime().UnixNano()
	if v, ok := s.renders.Load(path); ok {
		if ent := v.(*renderEntry); ent.publicURL == publicURL && ent.size == info.Size() && ent.modNano == modNano {
			return ent, nil
		}
	}
	data, err := s.layout.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body := data
	if render != nil {
		body, err = render(data)
		if err != nil {
			return nil, err
		}
	}
	ent := &renderEntry{
		publicURL: publicURL,
		size:      info.Size(),
		modNano:   modNano,
		body:      body,
		etag:      makeETag(body),
	}
	if len(body) >= constants.GzipMinBytes {
		ent.gz = gzipData(body)
	}
	s.renders.Store(path, ent)
	return ent, nil
}

// verifyFresh verifies path against spec, skipping the hash when the file's
// size and mtime match a previously verified read.
func (s *Server) verifyFresh(d *safepath.Dir, path string, spec integrity.Spec) error {
	info, err := d.Stat(path)
	if err != nil {
		return err
	}
	sig := fileSig{size: info.Size(), modNano: info.ModTime().UnixNano()}
	s.verifyMu.Lock()
	cached, ok := s.verified[path]
	s.verifyMu.Unlock()
	if ok && cached == sig {
		return nil
	}
	if err := integrity.VerifyFile(d, path, spec); err != nil {
		s.verifyMu.Lock()
		delete(s.verified, path)
		s.verifyMu.Unlock()
		return err
	}
	s.noteVerified(path, sig)
	return nil
}

func (s *Server) noteVerified(path string, sig fileSig) {
	s.verifyMu.Lock()
	if len(s.verified) >= constants.VerifyCacheMaxEntries {
		clear(s.verified)
	}
	s.verified[path] = sig
	s.verifyMu.Unlock()
}

func (s *Server) noteVerifiedFile(d *safepath.Dir, path string) {
	info, err := d.Stat(path)
	if err != nil {
		return
	}
	s.noteVerified(path, fileSig{size: info.Size(), modNano: info.ModTime().UnixNano()})
}

func makeETag(body []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(body)
	return "\"" + strconv.Itoa(len(body)) + "-" + strconv.FormatUint(h.Sum64(), 16) + "\""
}

func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for part := range strings.SplitSeq(header, ",") {
		t := strings.TrimSpace(part)
		t = strings.TrimPrefix(t, "W/")
		if t == "*" || t == etag {
			return true
		}
	}
	return false
}

func isSHA1Hex(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// emitTagged writes a response with Cache-Control, ETag, and gzip handling,
// short circuiting to 304 when the client's If-None-Match matches. gz is a
// precomputed gzip body from a render/meta cache, or nil to compress lazily.
func (s *Server) emitTagged(w http.ResponseWriter, r *http.Request, data, gz []byte, cacheHit bool, cc, etag string) int64 {
	h := w.Header()
	h[constants.HeaderVary] = []string{constants.HeaderAcceptEncoding}
	if cc != "" {
		h[constants.HeaderCacheControl] = []string{cc}
	}
	if etag != "" {
		h[constants.HeaderETag] = []string{etag}
		if etagMatches(r.Header.Get(constants.HeaderIfNoneMatch), etag) {
			w.WriteHeader(http.StatusNotModified)
			s.stats.RecordServe(0, cacheHit)
			return 0
		}
	}
	if gzipEligible(r, data) {
		if gz == nil {
			gz = gzipData(data)
		}
		if len(gz) > 0 {
			h[constants.HeaderContentEncoding] = []string{"gzip"}
			if etag != "" {
				h[constants.HeaderETag] = []string{"W/" + etag}
			}
			return s.emitBytes(w, r, gz, cacheHit)
		}
	}
	return s.emitBytes(w, r, data, cacheHit)
}
