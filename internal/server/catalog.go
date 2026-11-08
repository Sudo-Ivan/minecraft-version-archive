package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/fabric"
	"minecraft-version-archive/internal/mojang"
	"minecraft-version-archive/internal/temurin"
	"minecraft-version-archive/internal/vfs"
)

// catalogArtifact is one downloadable file attached to an archived version:
// a client or server jar. URL is a root-relative mirror path; Archived reports
// whether the bytes are already on disk (a proxy miss may still serve them).
type catalogArtifact struct {
	URL      string `json:"url"`
	SHA1     string `json:"sha1,omitempty"`
	Size     int64  `json:"size"`
	Archived bool   `json:"archived"`
}

// catalogAsset is the version's asset index reference.
type catalogAsset struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	TotalSize int64  `json:"totalSize,omitempty"`
	Archived  bool   `json:"archived"`
}

// catalogJava is the Java runtime a version expects, plus the Temurin mirror
// path when that major is archived locally.
type catalogJava struct {
	Major     int    `json:"major"`
	Component string `json:"component,omitempty"`
	Temurin   string `json:"temurin,omitempty"`
}

// catalogEntry is one archived version's download sheet, served both as the
// /versions JSON catalog row and as the data behind the version page.
type catalogEntry struct {
	ID          string           `json:"id"`
	Type        string           `json:"type,omitempty"`
	Time        string           `json:"time,omitempty"`
	ReleaseTime string           `json:"releaseTime,omitempty"`
	Page        string           `json:"page"`
	VersionJSON string           `json:"version_json"`
	Client      *catalogArtifact `json:"client,omitempty"`
	Server      *catalogArtifact `json:"server,omitempty"`
	AssetIndex  *catalogAsset    `json:"asset_index,omitempty"`
	Java        *catalogJava     `json:"java,omitempty"`
	Libraries   int              `json:"libraries"`
	Fabric      string           `json:"fabric_loader,omitempty"`
}

type catalogDoc struct {
	Name        string         `json:"name"`
	ProxyOnMiss bool           `json:"proxy_on_miss"`
	Versions    []catalogEntry `json:"versions"`
}

type catalogSnap struct {
	body   []byte
	gz     []byte
	etag   string
	byID   map[string]*catalogEntry
	expiry int64
}

// catalogDownload mirrors the sha1/size pair Mojang puts on every download
// entry. On-disk version JSONs are URL-rewritten but keep these fields, so
// they are the verification data a downloader should check against.
type catalogDownload struct {
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

// catalogVersionMeta is the slice of a version JSON the catalog needs.
type catalogVersionMeta struct {
	Type        string `json:"type"`
	Time        string `json:"time"`
	ReleaseTime string `json:"releaseTime"`
	Downloads   struct {
		Client *catalogDownload `json:"client"`
		Server *catalogDownload `json:"server"`
	} `json:"downloads"`
	AssetIndex struct {
		ID        string `json:"id"`
		TotalSize int64  `json:"totalSize"`
	} `json:"assetIndex"`
	JavaVersion *struct {
		MajorVersion int    `json:"majorVersion"`
		Component    string `json:"component"`
	} `json:"javaVersion"`
	Libraries []json.RawMessage `json:"libraries"`
}

type catalogFileCache struct {
	sig  fileSig
	meta *catalogVersionMeta
}

// catalogEntryFor returns the catalog row for id, resolving Mojang version
// aliases (1.5.0 -> 1.5) the same way the jar and version routes do.
func (s *Server) catalogEntryFor(id string) *catalogEntry {
	snap := s.cachedCatalog()
	if snap == nil {
		return nil
	}
	for _, candidate := range mojang.VersionIDCandidates(id) {
		if ent := snap.byID[candidate]; ent != nil {
			return ent
		}
	}
	return nil
}

func (s *Server) cachedCatalog() *catalogSnap {
	if snap := s.catalog.Load(); snap != nil && time.Now().UnixNano() < snap.expiry {
		return snap
	}
	snap := s.buildCatalog()
	if snap == nil {
		return nil
	}
	s.catalog.Store(snap)
	return snap
}

func (s *Server) buildCatalog() *catalogSnap {
	entries, err := s.layout.ReadDir(s.layout.VersionsDir())
	if err != nil {
		entries = nil
	}

	temurinMajors := map[int]bool{}
	if idx, err := temurin.ReadGlobalIndex(s.layout); err == nil {
		for _, m := range idx.Majors {
			temurinMajors[m] = true
		}
	}

	list := make([]*catalogEntry, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(ent.Name(), ".json")
		meta := s.catalogVersionMeta(ent)
		if meta == nil {
			continue
		}
		esc := url.PathEscape(id)
		entry := &catalogEntry{
			ID:          id,
			Type:        meta.Type,
			Time:        meta.Time,
			ReleaseTime: meta.ReleaseTime,
			Page:        "/versions/" + esc,
			VersionJSON: "/versions/" + esc + ".json",
			Libraries:   len(meta.Libraries),
		}
		entry.Client = s.catalogArtifact(id, constants.DirClient, meta.Downloads.Client)
		entry.Server = s.catalogArtifact(id, constants.DirServer, meta.Downloads.Server)
		if meta.AssetIndex.ID != "" {
			entry.AssetIndex = &catalogAsset{
				ID:        meta.AssetIndex.ID,
				URL:       "/assets/indexes/" + url.PathEscape(meta.AssetIndex.ID) + ".json",
				TotalSize: meta.AssetIndex.TotalSize,
				Archived:  s.layout.Exists(s.layout.AssetIndexPath(meta.AssetIndex.ID)),
			}
		}
		if meta.JavaVersion != nil {
			cj := &catalogJava{
				Major:     meta.JavaVersion.MajorVersion,
				Component: meta.JavaVersion.Component,
			}
			if temurinMajors[cj.Major] {
				cj.Temurin = "/java/temurin/" + strconv.Itoa(cj.Major)
			}
			entry.Java = cj
		}
		if rel := fabric.MetaStorageRel("v2/versions/loader/" + id); s.layout.Exists(s.layout.FabricMetaPath(rel)) {
			entry.Fabric = "/fabric/v2/versions/loader/" + esc
		}
		list = append(list, entry)
	}

	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.ReleaseTime != b.ReleaseTime {
			return a.ReleaseTime > b.ReleaseTime
		}
		return a.ID > b.ID
	})

	byID := make(map[string]*catalogEntry, len(list))
	for _, e := range list {
		byID[e.ID] = e
	}

	doc := catalogDoc{
		Name:        "minecraft-version-archive",
		ProxyOnMiss: s.proxy != nil,
		Versions:    make([]catalogEntry, len(list)),
	}
	for i, e := range list {
		doc.Versions[i] = *e
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	snap := &catalogSnap{
		body:   body,
		byID:   byID,
		expiry: time.Now().UnixNano() + int64(s.cfg.InventoryTTL)*int64(time.Second),
	}
	snap.etag = makeETag(body)
	if len(body) >= constants.GzipMinBytes {
		snap.gz = gzipData(body)
	}
	return snap
}

// catalogVersionMeta parses one versions/*.json file, cached by path and file
// signature so catalog rebuilds only re-read files that actually changed.
func (s *Server) catalogVersionMeta(ent vfs.DirEntry) *catalogVersionMeta {
	info, err := ent.Info()
	if err != nil {
		return nil
	}
	path := filepath.Join(s.layout.VersionsDir(), ent.Name())
	sig := fileSig{size: info.Size(), modNano: info.ModTime().UnixNano()}
	if v, ok := s.catalogFiles.Load(path); ok {
		if c := v.(*catalogFileCache); c.sig == sig {
			return c.meta
		}
	}
	data, err := s.layout.ReadFile(path)
	if err != nil {
		return nil
	}
	var meta catalogVersionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	s.catalogFiles.Store(path, &catalogFileCache{sig: sig, meta: &meta})
	return &meta
}

func (s *Server) catalogArtifact(id, kind string, dl *catalogDownload) *catalogArtifact {
	if dl == nil {
		return nil
	}
	var dir string
	switch kind {
	case constants.DirClient:
		dir = s.layout.ClientDir()
	case constants.DirServer:
		dir = s.layout.ServerDir()
	default:
		return nil
	}
	return &catalogArtifact{
		URL:      "/jars/" + kind + "/" + url.PathEscape(id) + constants.FileClientJar,
		SHA1:     dl.SHA1,
		Size:     dl.Size,
		Archived: s.layout.Exists(filepath.Join(dir, id+constants.FileClientJar)),
	}
}

func (s *Server) serveCatalog(w http.ResponseWriter, r *http.Request) {
	snap := s.cachedCatalog()
	if snap == nil {
		http.NotFound(w, r)
		return
	}
	w.Header()[constants.HeaderContentType] = constants.ContentTypeJSON
	s.emitTagged(w, r, snap.body, snap.gz, true, constants.CacheControlShort, snap.etag)
}
