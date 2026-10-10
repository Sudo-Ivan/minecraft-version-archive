package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/mojang"
	"minecraft-version-archive/internal/temurin"
)

type javaSnap struct {
	body []byte
}

var javaEntryPool = sync.Pool{New: func() any { s := make([]byte, 0, 96); return &s }}

func (s *Server) handleJavaIndex(w http.ResponseWriter, r *http.Request) {
	body := s.cachedJavaIndex()
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	s.emitTagged(w, r, body, nil, true, constants.CacheControlShort, "")
}

func (s *Server) handleJavaVersion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.Contains(id, "..") || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	body, ok := s.javaResponse(r.Context(), id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	s.emitTagged(w, r, body, nil, true, constants.CacheControlShort, "")
}

func (s *Server) cachedJavaIndex() []byte {
	now := time.Now().UnixNano()
	if now < s.javaExpiry.Load() {
		if snap := s.javaSnap.Load(); snap != nil {
			return snap.body
		}
	}
	body := s.buildJavaIndex()
	s.javaSnap.Store(&javaSnap{body: body})
	s.javaExpiry.Store(now + int64(s.cfg.InventoryTTL)*int64(time.Second))
	return body
}

func (s *Server) buildJavaIndex() []byte {
	entries, err := os.ReadDir(s.layout.VersionsDir())
	if err != nil {
		return []byte(`{"versions":[]}`)
	}
	buf := make([]byte, 0, len(entries)*72+16)
	buf = append(buf, `{"versions":[`...)
	first := true
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(ent.Name(), ".json")
		resp, ok := s.javaResponse(context.Background(), id)
		if !ok {
			continue
		}
		if !first {
			buf = append(buf, ',')
		}
		first = false
		buf = append(buf, resp...)
	}
	buf = append(buf, `]}`...)
	return buf
}

func (s *Server) javaResponse(ctx context.Context, id string) ([]byte, bool) {
	if v, ok := s.javaRespCache.Load(id); ok {
		return v.([]byte), true
	}
	data, resolved, ok := s.readVersionJSON(ctx, id)
	if !ok {
		return nil, false
	}
	info, ok := mojang.ExtractJavaVersion(data)
	if !ok {
		return nil, false
	}
	p := javaEntryPool.Get().(*[]byte)
	buf := (*p)[:0]
	buf = mojang.AppendJavaResponse(buf, resolved, info)
	if _, ok := temurin.ReadMajorManifest(s.layout, info.Major); ok {
		buf = temurin.AppendMCLink(buf, info.Major)
	}
	out := make([]byte, len(buf))
	copy(out, buf)
	*p = buf
	javaEntryPool.Put(p)
	s.javaRespCache.Store(resolved, out)
	if resolved != id {
		s.javaRespCache.Store(id, out)
	}
	return out, true
}

func (s *Server) readVersionJSON(ctx context.Context, id string) ([]byte, string, bool) {
	for _, candidate := range mojang.VersionIDCandidates(id) {
		path := s.layout.VersionJSON(candidate)
		data, err := s.layout.ReadFile(path)
		if err == nil {
			return data, candidate, true
		}
	}
	if s.proxy != nil {
		for _, candidate := range mojang.VersionIDCandidates(id) {
			data, err := s.proxy.versionJSON(ctx, candidate)
			if err == nil {
				return data, candidate, true
			}
		}
	}
	return nil, "", false
}
