package server

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/fabric"
	"minecraft-version-archive/internal/integrity"
)

func (s *Server) handleFabricMeta(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("path")
	if rel == "" || strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	base := s.publicURL(r)
	apiRel := fabric.MetaRequestRel(rel)
	path := s.layout.FabricMetaPath(fabric.MetaStorageRel(apiRel))
	render := func(data []byte) ([]byte, error) {
		return fabric.RewriteMeta(data, base), nil
	}
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	ent, err := s.renderFile(path, base, render)
	if err == nil {
		s.emitTagged(w, r, ent.body, ent.gz, true, constants.CacheControlShort, ent.etag)
		return
	}
	if s.proxy == nil {
		http.NotFound(w, r)
		return
	}
	data, err := s.proxy.fabricMeta(r.Context(), apiRel)
	if err != nil {
		s.recordProxyError("fabric meta proxy", err, "rel", apiRel)
		http.NotFound(w, r)
		return
	}
	_ = s.proxy.maybeCache(path, data, integrity.Spec{})
	out := fabric.RewriteMeta(data, base)
	s.emitTagged(w, r, out, nil, false, constants.CacheControlShort, makeETag(out))
}

func (s *Server) handleFabricMaven(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("path")
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if strings.HasPrefix(clean, "..") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.layout.FabricMavenDir(), clean)
	w.Header()[constants.HeaderCacheControl] = []string{constants.CacheControlImmutable}
	s.servePath(w, r, path, s.missBig(path, func(context.Context) (string, integrity.Spec, error) {
		return fabric.MavenURL(clean), integrity.Spec{}, nil
	}))
}
