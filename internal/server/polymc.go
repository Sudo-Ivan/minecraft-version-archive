package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/polymc"
)

// metaResponse is a cached PolyMC meta document: generated component indexes
// and version files are expensive to rebuild, so they live for a short TTL.
type metaResponse struct {
	body  []byte
	gz    []byte
	etag  string
	exp   time.Time
	found bool
}

func (s *Server) servePolyMCMeta(ctx context.Context, rel, mcVersion, publicURL string) (*metaResponse, error) {
	key := rel + "\x00" + mcVersion + "\x00" + publicURL
	if v, ok := s.metaResp.Load(key); ok {
		if ent := v.(*metaResponse); time.Now().Before(ent.exp) {
			return ent, nil
		}
	}
	ttl := time.Duration(s.cfg.InventoryTTL) * time.Second
	if ttl <= 0 {
		ttl = time.Duration(constants.DefaultInventoryTTL) * time.Second
	}
	ent, err := s.metaGroup.Do(ctx, key, func(context.Context) (*metaResponse, error) {
		if v, ok := s.metaResp.Load(key); ok {
			if e := v.(*metaResponse); time.Now().Before(e.exp) {
				return e, nil
			}
		}
		data, ok, err := polymc.ServeMeta(s.layout, rel, mcVersion, publicURL)
		if err != nil {
			return nil, err
		}
		if !ok {
			return &metaResponse{exp: time.Now().Add(ttl)}, nil
		}
		e := &metaResponse{
			body:  data,
			etag:  makeETag(data),
			exp:   time.Now().Add(ttl),
			found: true,
		}
		if len(data) >= constants.GzipMinBytes {
			e.gz = gzipData(data)
		}
		s.metaResp.Store(key, e)
		return e, nil
	})
	if err != nil {
		return nil, err
	}
	return ent, nil
}

func (s *Server) handlePolyMCMeta(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("path")
	if strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	mcVersion := r.URL.Query().Get("minecraft")
	publicURL := s.publicURL(r)
	ent, err := s.servePolyMCMeta(r.Context(), rel, mcVersion, publicURL)
	if err == nil && ent.found {
		w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
		s.emitTagged(w, r, ent.body, ent.gz, false, constants.CacheControlShort, ent.etag)
		return
	}
	if s.proxy != nil {
		data, ok, err := s.proxyPolyMCMeta(r.Context(), rel, mcVersion, publicURL)
		if err != nil {
			s.recordProxyError("polymc meta proxy", err, "path", rel, "minecraft", mcVersion)
			http.NotFound(w, r)
			return
		}
		if ok {
			w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
			s.emitTagged(w, r, data, nil, false, constants.CacheControlShort, "")
			return
		}
	}
	http.NotFound(w, r)
}
