package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
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
}

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
	idx.prune()
	idx.backfillAssets()
	return idx, nil
}

func (h *HashIndex) Lookup(spec integrity.Spec) (string, bool) {
	if !spec.Known() {
		return "", false
	}
	key := normSHA1(spec.SHA1)
	dir, err := h.layout.Dir()
	if err != nil {
		return "", false
	}

	h.mu.RLock()
	entry, ok := h.entries[key]
	h.mu.RUnlock()

	if ok {
		if err := integrity.VerifyFile(dir, entry.Path, spec); err == nil {
			return entry.Path, true
		}
		h.remove(key)
	}

	fallback := h.layout.AssetObjectPath(spec.SHA1)
	if h.layout.Exists(fallback) {
		if err := integrity.VerifyFile(dir, fallback, spec); err == nil {
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
	dir, err := h.layout.Dir()
	if err != nil {
		return
	}
	if err := integrity.VerifyFile(dir, path, spec); err != nil {
		return
	}
	h.record(normSHA1(spec.SHA1), path)
}

func (h *HashIndex) record(key, path string) {
	size := fileSize(path)
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.entries[key]; ok && cur.Path == path && cur.Size == size {
		return
	}
	h.entries[key] = HashEntry{Path: path, Size: size}
	_ = h.saveLocked()
}

func (h *HashIndex) remove(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.entries[key]; !ok {
		return
	}
	delete(h.entries, key)
	_ = h.saveLocked()
}

func (h *HashIndex) prune() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, entry := range h.entries {
		if _, err := os.Stat(entry.Path); err != nil {
			delete(h.entries, key)
		}
	}
	_ = h.saveLocked()
}

func (h *HashIndex) backfillAssets() {
	dir, err := h.layout.Dir()
	if err != nil {
		return
	}
	root := h.layout.AssetObjectsDir()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		hash := info.Name()
		if len(hash) != 40 {
			return nil
		}
		spec := integrity.Spec{SHA1: hash, Size: info.Size()}
		if integrity.VerifyFile(dir, path, spec) != nil {
			return nil
		}
		h.record(normSHA1(hash), path)
		return nil
	})
}

func (h *HashIndex) saveLocked() error {
	data, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir, err := h.layout.Dir()
	if err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := dir.WriteFile(tmp, data, safepath.FilePerm); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

func normSHA1(sha1 string) string {
	return strings.ToLower(strings.TrimSpace(sha1))
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}
