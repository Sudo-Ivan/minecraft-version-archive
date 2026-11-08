package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/vfs"
)

type HashEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
}

type HashIndex struct {
	path    string
	layout  *Layout
	mu      sync.RWMutex
	entries map[string]HashEntry

	saveMu    sync.Mutex
	saveTimer *time.Timer
}

// indexSaveDelay coalesces index writes on remote backends: a burst of
// Record calls produces one object upload instead of one per entry. Entries
// are re-derivable, so a crash before flush only costs a re-backfill.
const indexSaveDelay = 1500 * time.Millisecond

func OpenHashIndex(layout *Layout) (*HashIndex, error) {
	path := filepath.Join(layout.InventoryDir(), constants.FileHashIndex)
	idx := &HashIndex{
		path:    path,
		layout:  layout,
		entries: make(map[string]HashEntry),
	}
	if data, err := layout.ReadFile(path); err == nil {
		var stored map[string]HashEntry
		if json.Unmarshal(data, &stored) == nil {
			idx.entries = stored
		}
	}
	idx.normalizePaths()
	idx.prune()
	idx.backfillAssets()
	return idx, nil
}

// normalizePaths converts legacy absolute entry paths to root-relative
// slash paths so the index is portable across hosts sharing a backend.
func (h *HashIndex) normalizePaths() {
	for key, entry := range h.entries {
		rel, ok := h.layout.RelPath(entry.Path)
		if !ok {
			delete(h.entries, key)
			continue
		}
		entry.Path = rel
		h.entries[key] = entry
	}
}

func (h *HashIndex) Lookup(spec integrity.Spec) (string, bool) {
	if !spec.Known() {
		return "", false
	}
	key := normSHA1(spec.SHA1)
	fsys, err := h.layout.FS()
	if err != nil {
		return "", false
	}

	h.mu.RLock()
	entry, ok := h.entries[key]
	h.mu.RUnlock()

	if ok {
		abs := h.layout.Path(entry.Path)
		if err := integrity.VerifyFile(fsys, abs, spec); err == nil {
			return abs, true
		}
		h.remove(key)
	}

	fallback := h.layout.AssetObjectPath(spec.SHA1)
	if h.layout.Exists(fallback) {
		if err := integrity.VerifyFile(fsys, fallback, spec); err == nil {
			h.record(key, fallback)
			return fallback, true
		}
	}
	return "", false
}

func (h *HashIndex) Record(spec integrity.Spec, path string) {
	if !spec.Known() || path == "" {
		return
	}
	fsys, err := h.layout.FS()
	if err != nil {
		return
	}
	if err := integrity.VerifyFile(fsys, path, spec); err != nil {
		return
	}
	h.record(normSHA1(spec.SHA1), path)
}

func (h *HashIndex) record(key, path string) {
	rel, ok := h.layout.RelPath(path)
	if !ok {
		return
	}
	size := h.fileSize(path)
	h.mu.Lock()
	changed := false
	if cur, ok := h.entries[key]; !ok || cur.Path != rel || cur.Size != size {
		h.entries[key] = HashEntry{Path: rel, Size: size}
		changed = true
	}
	h.mu.Unlock()
	if changed {
		h.requestSave()
	}
}

func (h *HashIndex) remove(key string) {
	h.mu.Lock()
	_, ok := h.entries[key]
	if ok {
		delete(h.entries, key)
	}
	h.mu.Unlock()
	if ok {
		h.requestSave()
	}
}

// requestSave persists the index. Local backends save synchronously (cheap
// atomic rename); remote backends debounce so bursts coalesce into one
// upload.
func (h *HashIndex) requestSave() {
	fsys, err := h.layout.FS()
	if err != nil {
		return
	}
	if remote, ok := fsys.(vfs.RemoteFS); !ok || !remote.IsRemote() {
		_ = h.save()
		return
	}
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	if h.saveTimer == nil {
		h.saveTimer = time.AfterFunc(indexSaveDelay, func() {
			h.saveMu.Lock()
			h.saveTimer = nil
			h.saveMu.Unlock()
			_ = h.save()
		})
	}
}

// Save flushes any pending debounced index write.
func (h *HashIndex) Save() {
	h.saveMu.Lock()
	t := h.saveTimer
	h.saveTimer = nil
	h.saveMu.Unlock()
	if t != nil {
		t.Stop()
	}
	_ = h.save()
}

func (h *HashIndex) prune() {
	// Remote backends can answer "which keys exist" with one listing;
	// local backends stat each entry (cheap on disk).
	if ks, ok := h.backendKeySet(); ok {
		h.mu.Lock()
		for key, entry := range h.entries {
			if _, exists := ks[entry.Path]; !exists {
				delete(h.entries, key)
			}
		}
		h.mu.Unlock()
		_ = h.save()
		return
	}
	fsys, err := h.layout.FS()
	if err != nil {
		return
	}
	h.mu.Lock()
	for key, entry := range h.entries {
		if _, err := fsys.Stat(h.layout.Path(entry.Path)); err != nil {
			delete(h.entries, key)
		}
	}
	h.mu.Unlock()
	_ = h.save()
}

func (h *HashIndex) backendKeySet() (map[string]struct{}, bool) {
	fsys, err := h.layout.FS()
	if err != nil {
		return nil, false
	}
	ks, ok := fsys.(vfs.KeySeter)
	if !ok {
		return nil, false
	}
	set, err := ks.KeySet()
	if err != nil {
		return nil, false
	}
	return set, true
}

func (h *HashIndex) backfillAssets() {
	fsys, err := h.layout.FS()
	if err != nil {
		return
	}
	root := h.layout.AssetObjectsDir()
	_ = h.layout.Walk(root, func(path string, d vfs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		hash := info.Name()
		if len(hash) != 40 {
			return nil
		}
		key := normSHA1(hash)

		h.mu.RLock()
		_, indexed := h.entries[key]
		h.mu.RUnlock()
		if indexed {
			return nil
		}

		spec := integrity.Spec{SHA1: hash, Size: info.Size()}
		// When the listing carried stored checksums (object metadata) we can
		// verify without any extra request.
		if di, ok := info.(vfs.DigestInfo); ok {
			if sha1hex, _, ok2 := di.StoredDigest(); ok2 && strings.EqualFold(sha1hex, hash) {
				h.record(key, path)
				return nil
			}
		}
		if integrity.VerifyFile(fsys, path, spec) != nil {
			return nil
		}
		h.record(key, path)
		return nil
	})
}

func (h *HashIndex) save() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	data, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	fsys, err := h.layout.FS()
	if err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := fsys.WriteFile(tmp, data, safepath.FilePerm); err != nil {
		return err
	}
	return fsys.Rename(tmp, h.path)
}

func (h *HashIndex) fileSize(path string) int64 {
	fsys, err := h.layout.FS()
	if err != nil {
		return 0
	}
	st, err := fsys.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func normSHA1(sha1 string) string {
	return strings.ToLower(strings.TrimSpace(sha1))
}
