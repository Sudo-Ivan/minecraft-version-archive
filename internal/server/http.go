package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/cache"
	"minecraft-version-archive/internal/coalesce"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/fetch"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/mojang"
	"minecraft-version-archive/internal/p2p"
	"minecraft-version-archive/internal/polymc"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

var (
	responseWriterPool = sync.Pool{New: func() any { return &responseWriter{} }}
	headerBufPool      = sync.Pool{New: func() any { b := make([]byte, 0, 64); return &b }}
)

type inventorySnap struct {
	inv      p2p.Inventory
	versions int
}

type Server struct {
	cfg           *config.Config
	layout        *store.Layout
	cache         *cache.Memory
	loader        *coalesce.Loader
	invGroup      *coalesce.Group[p2p.Inventory]
	archiver      *archive.Archiver
	proxy         *proxyHelper
	stats         *stats.Collector
	log           *slog.Logger
	mux           *http.ServeMux
	httpSrv       *http.Server
	debugLog      bool
	invSnap       atomic.Pointer[inventorySnap]
	invExpiry     atomic.Int64
	javaSnap      atomic.Pointer[javaSnap]
	javaExpiry    atomic.Int64
	javaRespCache sync.Map
	renders       sync.Map
	metaGroup     *coalesce.Group[*metaResponse]
	metaResp      sync.Map
	verifyMu      sync.Mutex
	verified      map[string]fileSig
}

func New(cfg *config.Config, archiver *archive.Archiver, log *slog.Logger) *Server {
	st := archiver.Stats()
	if st == nil {
		st = stats.New()
	}
	if err := st.EnableFileStore(cfg.DataDir); err != nil && log != nil {
		log.Warn("stats file store", "err", err)
	}
	var mem *cache.Memory
	if cfg.CacheEnabled {
		mem = cache.NewSharded(cfg.CacheEntries, cfg.CacheBytes, cfg.CacheShards)
	}
	s := &Server{
		cfg:       cfg,
		layout:    store.New(cfg.DataDir),
		cache:     mem,
		loader:    coalesce.New(),
		invGroup:  coalesce.NewGroup[p2p.Inventory](),
		metaGroup: coalesce.NewGroup[*metaResponse](),
		archiver:  archiver,
		stats:     st,
		log:       log,
		mux:       http.NewServeMux(),
		debugLog:  log.Enabled(context.Background(), slog.LevelDebug),
		verified:  make(map[string]fileSig),
	}
	if cfg.ProxyOnMiss {
		s.proxy = newProxyHelper(s.layout, cfg.RetryMax, cfg.RetryBaseMS, cfg.ProxyCache, log)
	}
	retryMax := cfg.RetryMax
	if retryMax < 1 {
		retryMax = constants.DefaultRetryMax
	}
	metaFetch := fetch.New(retryMax, cfg.RetryBaseMS)
	polymc.SetReleaseTimesFetcher(func(ctx context.Context, url string) ([]byte, error) {
		if s.proxy != nil {
			return s.proxy.getBytes(ctx, url)
		}
		return metaFetch.Get(ctx, url)
	})
	s.routes()
	s.httpSrv = &http.Server{
		Addr:              cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: constants.ReadHeaderTimeout,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       constants.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
	}
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc(constants.RouteRoot, s.handleRoot)
	s.mux.HandleFunc(constants.RouteHealth, s.handleHealth)
	if s.cfg.StatsEnabled {
		s.mux.HandleFunc(constants.RouteStats, s.handleStats)
	}
	s.mux.HandleFunc(constants.RouteInventory, s.handleInventory)
	s.mux.HandleFunc(constants.RouteJavaIndex, s.handleJavaIndex)
	s.mux.HandleFunc(constants.RouteJavaTemurinIdx, s.handleJavaTemurinIndex)
	s.mux.HandleFunc(constants.RouteJavaTemurinBin, s.handleJavaTemurinBinary)
	s.mux.HandleFunc(constants.RouteJavaTemurin, s.handleJavaTemurinMajor)
	s.mux.HandleFunc(constants.RouteJavaVersion, s.handleJavaVersion)
	s.mux.HandleFunc(constants.RouteManifest, s.handleManifest)
	s.mux.HandleFunc(constants.RouteVersions, s.handleVersions)
	s.mux.HandleFunc(constants.RouteJars, s.handleJars)
	s.mux.HandleFunc(constants.RouteLibraries, s.handleLibraries)
	s.mux.HandleFunc(constants.RouteAssetIdx, s.handleAssetIndexes)
	s.mux.HandleFunc(constants.RouteAssetObj, s.handleAssetObject)
	s.mux.HandleFunc(constants.RouteFabricMeta, s.handleFabricMeta)
	s.mux.HandleFunc(constants.RouteFabricMaven, s.handleFabricMaven)
	s.mux.HandleFunc(constants.RoutePolyMCMeta, s.handlePolyMCMeta)
}

func (s *Server) publicURL(r *http.Request) string {
	return config.RequestPublicURL(r, s.cfg.PublicURL, s.cfg.TrustProxy)
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	if tc, ok := ln.(*net.TCPListener); ok {
		_ = tc.SetDeadline(time.Time{})
	}
	s.log.Info("listening", "addr", s.cfg.Listen, "public", s.cfg.PublicURL, "cache_shards", s.cfg.CacheShards, "proxy_on_miss", s.cfg.ProxyOnMiss, "proxy_cache", s.cfg.ProxyCache)
	return s.httpSrv.Serve(ln)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	base := s.publicURL(r)
	out, err := json.Marshal(map[string]any{
		"name":       "minecraft-version-archive",
		"public_url": base,
		"endpoints": map[string]string{
			"health":       "/health",
			"manifest":     "/mc/game/version_manifest_v2.json",
			"versions":     "/versions/{file}",
			"java":         "/java",
			"java_version": "/java/{id}",
			"java_temurin": "/java/temurin",
			"temurin":      "/java/temurin/{major}",
			"jars":         "/jars/{kind}/{file}",
			"libraries":    "/libraries/{path}",
			"assets":       "/assets/indexes/{file}",
			"objects":      "/assets/objects/{prefix}/{hash}",
			"fabric_meta":  "/fabric/v2/{path}",
			"fabric_maven": "/maven/fabricmc/{path}",
			"polymc_meta":  "/meta/v1/{path}",
		},
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	s.emitTagged(w, r, out, nil, false, constants.CacheControlShort, "")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	s.emitTagged(w, r, constants.HealthBody, nil, false, constants.CacheControlNoStore, "")
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	w.Header()[constants.HeaderCacheControl] = []string{constants.CacheControlNoStore}
	entries, bytes := 0, int64(0)
	if s.cache != nil {
		entries, bytes = s.cache.Stats()
	}
	_ = s.stats.WriteJSON(w, entries, bytes, s.cfg.CacheBytes, s.versionCount())
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	inv := s.cachedInventory(r.Context())
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	w.Header()[constants.HeaderCacheControl] = []string{constants.CacheControlShort}
	_ = p2p.WriteInventory(w, inv)
}

func (s *Server) cachedInventory(ctx context.Context) p2p.Inventory {
	now := time.Now().UnixNano()
	if now < s.invExpiry.Load() {
		if snap := s.invSnap.Load(); snap != nil {
			return snap.inv
		}
	}
	inv, err := s.invGroup.Do(ctx, "inventory", func(context.Context) (p2p.Inventory, error) {
		if time.Now().UnixNano() < s.invExpiry.Load() {
			if snap := s.invSnap.Load(); snap != nil {
				return snap.inv, nil
			}
		}
		built := s.archiver.BuildInventory()
		s.invSnap.Store(&inventorySnap{inv: built, versions: len(built.Versions)})
		s.invExpiry.Store(time.Now().UnixNano() + int64(s.cfg.InventoryTTL)*int64(time.Second))
		return built, nil
	})
	if err != nil {
		if snap := s.invSnap.Load(); snap != nil {
			return snap.inv
		}
		return p2p.Inventory{}
	}
	return inv
}

func (s *Server) versionCount() int {
	if snap := s.invSnap.Load(); snap != nil {
		return snap.versions
	}
	return s.archiver.VersionCount()
}

func (s *Server) recordProxyError(event string, err error, attrs ...any) {
	if err == nil {
		return
	}
	s.stats.RecordError()
	args := make([]any, 0, 2+len(attrs))
	args = append(args, "err", err)
	args = append(args, attrs...)
	s.log.Warn(event, args...)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	base := s.publicURL(r)
	render := func(data []byte) ([]byte, error) {
		var manifest mojang.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, err
		}
		return json.Marshal(mojang.RewriteManifest(&manifest, base))
	}
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	ent, err := s.renderFile(s.layout.ManifestPath(), base, render)
	if err == nil {
		s.emitTagged(w, r, ent.body, ent.gz, true, constants.CacheControlShort, ent.etag)
		return
	}
	if s.proxy == nil {
		http.NotFound(w, r)
		return
	}
	data, err := s.proxy.getBytes(r.Context(), constants.MojangManifest)
	if err != nil {
		s.recordProxyError("manifest proxy", err)
		http.NotFound(w, r)
		return
	}
	body, err := render(data)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = s.proxy.maybeCache(s.layout.ManifestPath(), data, integrity.Spec{})
	s.emitTagged(w, r, body, nil, false, constants.CacheControlShort, makeETag(body))
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if file == "" || strings.Contains(file, "..") || strings.ContainsAny(file, `/\`) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.layout.VersionsDir(), file)
	id := strings.TrimSuffix(file, ".json")
	if strings.HasSuffix(file, ".json") {
		base := s.publicURL(r)
		render := func(data []byte) ([]byte, error) {
			return mojang.RewriteVersionJSON(data, base, id)
		}
		w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
		ent, err := s.renderFile(path, base, render)
		if err == nil {
			s.emitTagged(w, r, ent.body, ent.gz, true, constants.CacheControlShort, ent.etag)
			return
		}
		if _, statErr := s.layout.Stat(path); statErr == nil {
			s.servePath(w, r, path)
			return
		}
		if s.proxy == nil {
			http.NotFound(w, r)
			return
		}
		data, err := s.proxy.versionJSON(r.Context(), id)
		if err != nil {
			s.recordProxyError("version proxy", err, "id", id)
			http.NotFound(w, r)
			return
		}
		_ = s.proxy.maybeCache(path, data, integrity.Spec{})
		out, err := render(data)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		s.emitTagged(w, r, out, nil, false, constants.CacheControlShort, makeETag(out))
		return
	}
	if _, err := s.layout.Stat(path); err != nil {
		if s.proxy == nil {
			http.NotFound(w, r)
			return
		}
		data, err := s.proxy.versionJSON(r.Context(), id)
		if err != nil {
			s.recordProxyError("version proxy", err, "id", id)
			http.NotFound(w, r)
			return
		}
		_ = s.proxy.maybeCache(path, data, integrity.Spec{})
	}
	s.servePath(w, r, path)
}

func (s *Server) handleJars(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	file := r.PathValue("file")
	if file == "" || strings.Contains(file, "..") || strings.ContainsAny(file, `/\`) {
		http.NotFound(w, r)
		return
	}
	var dir string
	switch kind {
	case constants.DirClient:
		dir = s.layout.ClientDir()
	case constants.DirServer:
		dir = s.layout.ServerDir()
	default:
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(dir, file)
	id := strings.TrimSuffix(file, ".jar")
	w.Header()[constants.HeaderCacheControl] = []string{constants.CacheControlImmutable}
	s.servePath(w, r, path, s.missBig(path, func(ctx context.Context) (string, integrity.Spec, error) {
		return s.proxy.jarTarget(ctx, kind, id)
	}))
}

func (s *Server) handleLibraries(w http.ResponseWriter, r *http.Request) {
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
	path := filepath.Join(s.layout.LibrariesDir(), clean)
	w.Header()[constants.HeaderCacheControl] = []string{constants.CacheControlImmutable}
	s.servePath(w, r, path, s.missBig(path, func(context.Context) (string, integrity.Spec, error) {
		return mojang.LibraryUpstreamURL(clean), integrity.Spec{}, nil
	}))
}

func (s *Server) handleAssetIndexes(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if file == "" || strings.Contains(file, "..") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.layout.AssetIndexesDir(), file)
	h := w.Header()
	h[constants.HeaderCacheControl] = []string{constants.CacheControlImmutable}
	h[constants.HeaderContentType] = constants.ContentTypeJSON
	// Asset indexes are immutable JSON; serving them through the render cache
	// picks up ETag, 304, and precomputed gzip for free.
	if ent, err := s.renderFile(path, s.publicURL(r), nil); err == nil {
		s.emitTagged(w, r, ent.body, ent.gz, false, constants.CacheControlImmutable, ent.etag)
		return
	}
	s.missBytes(func(ctx context.Context) ([]byte, string, integrity.Spec, error) {
		data, dest, err := s.proxy.assetIndex(ctx, file)
		return data, dest, integrity.Spec{}, err
	})(w, r)
}

func (s *Server) handleAssetObject(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isSHA1Hex(hash) {
		http.NotFound(w, r)
		return
	}
	etag := "\"" + hash + "\""
	h := w.Header()
	h[constants.HeaderCacheControl] = []string{constants.CacheControlImmutable}
	h[constants.HeaderETag] = []string{etag}
	if etagMatches(r.Header.Get(constants.HeaderIfNoneMatch), etag) {
		w.WriteHeader(http.StatusNotModified)
		s.stats.RecordServe(0, true)
		return
	}
	path := s.layout.AssetObjectPath(hash)
	spec := integrity.Spec{SHA1: hash}
	dataDir, err := s.layout.Dir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	verr := s.verifyFresh(dataDir, path, spec)
	if verr == nil {
		s.servePath(w, r, path)
		return
	}
	if s.proxy == nil {
		s.log.Warn("asset integrity failed", "hash", hash, "err", verr)
		http.NotFound(w, r)
		return
	}
	url := mojang.AssetObjectUpstreamURL(hash)
	if s.proxy.proxyCache && s.proxy.hasDataDir {
		if err := s.proxy.download(r.Context(), url, path, spec); err != nil {
			s.recordProxyError("asset proxy", err, "hash", hash)
			http.NotFound(w, r)
			return
		}
		s.noteVerifiedFile(dataDir, path)
		s.servePath(w, r, path)
		return
	}
	data, fetchErr := s.proxy.getBytes(r.Context(), url)
	if fetchErr != nil || integrity.VerifyBytes(data, spec) != nil {
		if fetchErr != nil {
			s.recordProxyError("asset proxy", fetchErr, "hash", hash)
		} else {
			s.recordProxyError("asset proxy verify", integrity.VerifyBytes(data, spec), "hash", hash)
		}
		http.NotFound(w, r)
		return
	}
	s.emitBytes(w, r, data, false)
}

// missHandler writes a full response for a file missing from disk and returns
// true, or returns false to let servePath answer 404.
type missHandler func(w http.ResponseWriter, r *http.Request) (handled bool)

// missBytes adapts a buffered upstream fetch (metadata, JSON) into a
// missHandler, persisting the bytes to dest when the proxy cache is on.
func (s *Server) missBytes(fetch func(ctx context.Context) ([]byte, string, integrity.Spec, error)) missHandler {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if s.proxy == nil {
			return false
		}
		data, dest, spec, err := fetch(r.Context())
		if err != nil {
			s.recordProxyError("proxy miss", err, "path", dest)
			return false
		}
		_ = s.proxy.maybeCache(dest, data, spec)
		s.emitBytesJSON(w, r, data, false)
		return true
	}
}

// missBig resolves an upstream URL lazily, then either downloads to disk
// (proxy_cache on) or streams the response through (passthrough), so large
// files are never buffered in memory.
func (s *Server) missBig(dest string, target func(ctx context.Context) (string, integrity.Spec, error)) missHandler {
	return func(w http.ResponseWriter, r *http.Request) bool {
		p := s.proxy
		if p == nil {
			return false
		}
		url, spec, err := target(r.Context())
		if err != nil {
			s.recordProxyError("proxy resolve", err, "path", dest)
			return false
		}
		if p.proxyCache && p.hasDataDir {
			if err := p.download(r.Context(), url, dest, spec); err != nil {
				s.recordProxyError("proxy fetch", err, "path", dest)
				return false
			}
			s.servePath(w, r, dest)
			return true
		}
		n, err := p.stream(r.Context(), url, w, r)
		if err != nil {
			s.recordProxyError("proxy stream", err, "path", dest)
			return n > 0
		}
		s.stats.RecordServe(n, false)
		return true
	}
}

func (s *Server) servePath(w http.ResponseWriter, r *http.Request, path string, onMiss ...missHandler) {
	if s.cache != nil {
		if data, ok := s.cache.Get(path); ok {
			s.emitBytes(w, r, data, true)
			return
		}
	}

	f, err := s.layout.OpenFile(path)
	if err != nil {
		if len(onMiss) > 0 && onMiss[0] != nil && onMiss[0](w, r) {
			return
		}
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	maxCache := min(s.cfg.CacheBytes/constants.CacheServeFraction, s.cacheMaxEntry())
	if s.cache != nil && info.Size() > 0 && info.Size() <= maxCache {
		data, err := s.loadCached(r.Context(), path, f, int(info.Size()))
		if err == nil {
			s.emitBytes(w, r, data, false)
			return
		}
	}

	rw := responseWriterPool.Get().(*responseWriter)
	rw.ResponseWriter = w
	rw.written = 0
	http.ServeContent(rw, r, info.Name(), info.ModTime(), f)
	s.stats.RecordServe(rw.written, false)
	rw.ResponseWriter = nil
	responseWriterPool.Put(rw)
}

// cacheMaxEntry is the largest file size that can actually stay cached: a
// file bigger than a single shard's byte budget would be evicted instantly
// after every fill, so serving it through the cache would waste the read.
func (s *Server) cacheMaxEntry() int64 {
	if s.cache == nil {
		return 0
	}
	return s.cache.MaxEntryBytes()
}

func (s *Server) loadCached(ctx context.Context, path string, f *os.File, size int) ([]byte, error) {
	return s.loader.Do(ctx, path, func(context.Context) ([]byte, error) {
		if s.cache != nil {
			if data, ok := s.cache.Get(path); ok {
				return data, nil
			}
		}
		data := make([]byte, size)
		if _, err := f.ReadAt(data, 0); err != nil {
			return nil, err
		}
		if s.cache != nil {
			s.cache.Put(path, data)
		}
		return data, nil
	})
}

type responseWriter struct {
	http.ResponseWriter
	written int64
}

func (rw *responseWriter) Write(p []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(p)
	rw.written += int64(n)
	return n, err
}

// ReadFrom delegates to the underlying writer so http.ServeContent keeps its
// sendfile path for *os.File sources, while still counting bytes for stats.
func (rw *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(r)
		rw.written += n
		return n, err
	}
	n, err := io.Copy(rw.ResponseWriter, r)
	rw.written += n
	return n, err
}

func (s *Server) emitBytes(w http.ResponseWriter, r *http.Request, data []byte, cacheHit bool) int64 {
	n := serveBytes(w, r, data)
	s.stats.RecordServe(n, cacheHit)
	return n
}

// emitBytesJSON is emitBytes for JSON bodies: it marks Vary and serves a
// gzipped body when the client accepts it.
func (s *Server) emitBytesJSON(w http.ResponseWriter, r *http.Request, data []byte, cacheHit bool) int64 {
	h := w.Header()
	h[constants.HeaderVary] = []string{constants.HeaderAcceptEncoding}
	if gzipEligible(r, data) {
		if gz := gzipData(data); len(gz) > 0 {
			h[constants.HeaderContentEncoding] = []string{"gzip"}
			return s.emitBytes(w, r, gz, cacheHit)
		}
	}
	return s.emitBytes(w, r, data, cacheHit)
}

func serveBytes(w http.ResponseWriter, r *http.Request, data []byte) int64 {
	h := w.Header()
	h[constants.HeaderAcceptRanges] = constants.AcceptRanges
	h[constants.HeaderContentLength] = []string{contentLengthHeader(len(data))}

	if r.Method == http.MethodHead {
		return 0
	}

	rangeHeader := r.Header.Get(constants.HeaderRange)
	if rangeHeader == "" {
		n, _ := w.Write(data) //#nosec G705 -- serves archived binary assets, not HTML.
		return int64(n)
	}

	start, end, ok := parseRange(rangeHeader, len(data))
	if !ok {
		h[constants.HeaderContentRange] = []string{"bytes */" + contentLengthHeader(len(data))}
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return 0
	}

	buf := headerBufPool.Get().(*[]byte)
	rangeVal := string(appendRangeHeader((*buf)[:0], start, end, len(data)))
	h[constants.HeaderContentRange] = []string{rangeVal}
	h[constants.HeaderContentLength] = []string{contentLengthHeader(end - start + 1)}
	headerBufPool.Put(buf)

	w.WriteHeader(http.StatusPartialContent)
	n, _ := w.Write(data[start : end+1]) //#nosec G705 -- serves archived binary assets, not HTML.
	return int64(n)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}
