package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/temurin"
)

func (s *Server) handleJavaTemurinIndex(w http.ResponseWriter, r *http.Request) {
	idx, err := temurin.ReadGlobalIndex(s.layout)
	if err != nil || len(idx.Majors) == 0 {
		w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
		s.emitTagged(w, r, []byte(`{"majors":[]}`), nil, true, constants.CacheControlShort, "")
		return
	}
	out, err := json.Marshal(idx)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	s.emitTagged(w, r, out, nil, true, constants.CacheControlShort, "")
}

func (s *Server) handleJavaTemurinMajor(w http.ResponseWriter, r *http.Request) {
	major, ok := parseJavaMajor(r.PathValue("major"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	man, ok := temurin.ReadMajorManifest(s.layout, major)
	if !ok {
		http.NotFound(w, r)
		return
	}
	base := s.publicURL(r)
	outMan := *man
	for i := range outMan.Platforms {
		e := &outMan.Platforms[i]
		platID := temurinPlatID(e)
		e.URL = base + "/java/temurin/" + strconv.Itoa(major) + "/" + platID + "/" + e.File
	}
	out, err := json.Marshal(outMan)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	s.emitTagged(w, r, out, nil, true, constants.CacheControlShort, "")
}

func (s *Server) handleJavaTemurinBinary(w http.ResponseWriter, r *http.Request) {
	major, ok := parseJavaMajor(r.PathValue("major"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	platform := r.PathValue("platform")
	file := r.PathValue("file")
	if platform == "" || file == "" || strings.Contains(file, "..") || strings.ContainsAny(file, `/\`) {
		http.NotFound(w, r)
		return
	}
	man, ok := temurin.ReadMajorManifest(s.layout, major)
	if !ok {
		http.NotFound(w, r)
		return
	}
	entry, ok := temurin.FindPlatformEntry(man, platform, file)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path := temurin.PlatformFilePath(s.layout, major, platform, file)
	dataDir, err := s.layout.Dir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	spec := integrity.FromSHA256(entry.Checksum.Hash, entry.Size)
	if err := s.verifyFresh(dataDir, path, spec); err != nil {
		s.log.Warn("java integrity failed", "major", major, "platform", platform, "file", file, "err", err)
		http.NotFound(w, r)
		return
	}
	w.Header()[constants.HeaderCacheControl] = []string{constants.CacheControlImmutable}
	s.servePath(w, r, path)
}

func parseJavaMajor(raw string) (int, bool) {
	if raw == "" || strings.Contains(raw, "..") {
		return 0, false
	}
	major, err := strconv.Atoi(raw)
	if err != nil || major < 1 {
		return 0, false
	}
	return major, true
}

func temurinPlatID(e *temurin.PlatformEntry) string {
	for _, p := range temurin.DefaultPlatforms {
		if p.RuntimeOS == e.RuntimeOS || (p.OS == e.OS && p.Arch == e.Arch) {
			return p.ID
		}
	}
	return e.RuntimeOS
}
