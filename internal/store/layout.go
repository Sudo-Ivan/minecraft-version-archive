package store

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/vfs"
)

type Layout struct {
	Root string
	fsys vfs.FS
	once sync.Once
	err  error
}

// S3Options configures an S3-compatible object storage backend.
type S3Options struct {
	Bucket       string
	Prefix       string
	Endpoint     string // host[:port] or URL; empty means AWS S3
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	Anonymous    bool // never sign requests (public buckets)
	PathStyle    bool // path-style URLs (bucket in path)
	Secure       bool // use HTTPS when Endpoint carries no scheme
	SecureAuto   bool // derive Secure from endpoint scheme / localhost
	Redirect     bool // serve files via presigned redirects
	RedirectTTL  time.Duration
}

func New(root string) *Layout {
	return &Layout{Root: filepath.Clean(root)}
}

func NewS3(root string, opts S3Options) (*Layout, error) {
	fsys, err := newS3FS(root, opts)
	if err != nil {
		return nil, err
	}
	l := New(root)
	l.fsys = fsys
	return l, nil
}

// NewFailed returns a Layout whose backend always fails with err. Used when
// backend construction fails so callers surface the error instead of
// silently falling back to the local disk.
func NewFailed(root string, err error) *Layout {
	l := New(root)
	l.fsys = failedFS{err: err}
	return l
}

func NewForConfig(cfg *config.Config) (*Layout, error) {
	if !cfg.S3.Enabled {
		return New(cfg.DataDir), nil
	}
	return NewS3(cfg.DataDir, S3Options{
		Bucket:       cfg.S3.Bucket,
		Prefix:       cfg.S3.Prefix,
		Endpoint:     cfg.S3.Endpoint,
		Region:       cfg.S3.Region,
		AccessKey:    cfg.S3.AccessKey,
		SecretKey:    cfg.S3.SecretKey,
		SessionToken: cfg.S3.SessionToken,
		Anonymous:    cfg.S3.Anonymous,
		PathStyle:    cfg.S3.PathStyle,
		Secure:       cfg.S3.Secure,
		SecureAuto:   cfg.S3.SecureAuto,
		Redirect:     cfg.S3.Redirect,
		RedirectTTL:  cfg.S3.RedirectTTL,
	})
}

func (l *Layout) FS() (vfs.FS, error) {
	l.once.Do(func() {
		if l.fsys == nil {
			l.fsys, l.err = safepath.OpenDir(l.Root)
		}
	})
	if l.err != nil {
		return nil, l.err
	}
	return l.fsys, nil
}

type failedFS struct{ err error }

func (f failedFS) ReadFile(string) ([]byte, error) { return nil, f.err }
func (f failedFS) Open(string) (vfs.File, error)   { return nil, f.err }
func (f failedFS) Create(string) (vfs.File, error) { return nil, f.err }
func (f failedFS) OpenFileFlags(string, int, fs.FileMode) (vfs.File, error) {
	return nil, f.err
}
func (f failedFS) WriteFile(string, []byte, fs.FileMode) error { return f.err }
func (f failedFS) WriteFileExclusive(string, []byte, fs.FileMode) error {
	return f.err
}
func (f failedFS) MkdirAll(string, fs.FileMode) error     { return f.err }
func (f failedFS) Stat(string) (fs.FileInfo, error)       { return nil, f.err }
func (f failedFS) Exists(string) bool                     { return false }
func (f failedFS) Remove(string) error                    { return f.err }
func (f failedFS) Rename(string, string) error            { return f.err }
func (f failedFS) ReadDir(string) ([]vfs.DirEntry, error) { return nil, f.err }
func (f failedFS) Walk(string, vfs.WalkFunc) error        { return f.err }
func (f failedFS) CopyFromReader(string, io.Reader, int64) (int64, error) {
	return 0, f.err
}

func (l *Layout) ReadFile(abs string) ([]byte, error) {
	fsys, err := l.FS()
	if err != nil {
		return nil, err
	}
	return fsys.ReadFile(abs)
}

func (l *Layout) OpenFile(abs string) (vfs.File, error) {
	fsys, err := l.FS()
	if err != nil {
		return nil, err
	}
	return fsys.Open(abs)
}

func (l *Layout) Stat(abs string) (fs.FileInfo, error) {
	fsys, err := l.FS()
	if err != nil {
		return nil, err
	}
	return fsys.Stat(abs)
}

func (l *Layout) ReadDir(abs string) ([]vfs.DirEntry, error) {
	fsys, err := l.FS()
	if err != nil {
		return nil, err
	}
	return fsys.ReadDir(abs)
}

func (l *Layout) Walk(root string, fn vfs.WalkFunc) error {
	fsys, err := l.FS()
	if err != nil {
		return err
	}
	return fsys.Walk(root, fn)
}

func (l *Layout) Remove(abs string) error {
	fsys, err := l.FS()
	if err != nil {
		return err
	}
	return fsys.Remove(abs)
}

func (l *Layout) Rename(src, dst string) error {
	fsys, err := l.FS()
	if err != nil {
		return err
	}
	return fsys.Rename(src, dst)
}

func (l *Layout) MkdirAll(abs string, perm fs.FileMode) error {
	fsys, err := l.FS()
	if err != nil {
		return err
	}
	return fsys.MkdirAll(abs, perm)
}

func (l *Layout) Ensure() error {
	dirs := []string{
		l.ManifestDir(),
		l.VersionsDir(),
		l.ClientDir(),
		l.ServerDir(),
		l.LibrariesDir(),
		l.AssetIndexesDir(),
		l.AssetObjectsDir(),
		l.InventoryDir(),
		l.CoordinatorClaimsDir(),
		l.CoordinatorDoneDir(),
		l.FabricMetaDir(),
		l.FabricMavenDir(),
		l.JavaTemurinDir(),
	}
	fsys, err := l.FS()
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if err := fsys.MkdirAll(d, safepath.DirPerm); err != nil {
			return err
		}
	}
	return nil
}

func (l *Layout) ManifestDir() string { return filepath.Join(l.Root, constants.DirManifest) }
func (l *Layout) VersionsDir() string { return filepath.Join(l.Root, constants.DirVersions) }
func (l *Layout) ClientDir() string {
	return filepath.Join(l.Root, constants.DirJars, constants.DirClient)
}
func (l *Layout) ServerDir() string {
	return filepath.Join(l.Root, constants.DirJars, constants.DirServer)
}
func (l *Layout) LibrariesDir() string { return filepath.Join(l.Root, constants.DirLibraries) }
func (l *Layout) AssetIndexesDir() string {
	return filepath.Join(l.Root, constants.DirAssets, constants.DirIndexes)
}
func (l *Layout) AssetObjectsDir() string {
	return filepath.Join(l.Root, constants.DirAssets, constants.DirObjects)
}
func (l *Layout) InventoryDir() string   { return filepath.Join(l.Root, constants.DirInventory) }
func (l *Layout) CoordinatorDir() string { return filepath.Join(l.Root, constants.DirCoordinator) }
func (l *Layout) CoordinatorClaimsDir() string {
	return filepath.Join(l.CoordinatorDir(), constants.DirClaims)
}
func (l *Layout) CoordinatorDoneDir() string {
	return filepath.Join(l.CoordinatorDir(), constants.DirDone)
}

func (l *Layout) FabricDir() string {
	return filepath.Join(l.Root, constants.DirFabric)
}

func (l *Layout) FabricMetaDir() string {
	return filepath.Join(l.FabricDir(), constants.DirFabricMeta)
}

func (l *Layout) FabricMavenDir() string {
	return filepath.Join(l.FabricDir(), constants.DirFabricMaven)
}

func (l *Layout) FabricMetaPath(rel string) string {
	return filepath.Join(l.FabricMetaDir(), filepath.FromSlash(rel))
}

func (l *Layout) FabricMavenPath(rel string) string {
	return filepath.Join(l.FabricMavenDir(), filepath.FromSlash(rel))
}

func (l *Layout) JavaDir() string {
	return filepath.Join(l.Root, constants.DirJava)
}

func (l *Layout) JavaTemurinDir() string {
	return filepath.Join(l.JavaDir(), constants.DirTemurin)
}

func (l *Layout) JavaTemurinMajorDir(major int) string {
	return filepath.Join(l.JavaTemurinDir(), fmt.Sprintf("%d", major))
}

func (l *Layout) JavaTemurinPlatformPath(major int, platformID, file string) string {
	return filepath.Join(l.JavaTemurinMajorDir(major), platformID, file)
}

func (l *Layout) ManifestPath() string {
	return filepath.Join(l.ManifestDir(), constants.FileManifest)
}

func (l *Layout) VersionJSON(id string) string {
	return filepath.Join(l.VersionsDir(), id+constants.FileVersionJSON)
}

func (l *Layout) ClientJar(id string) string {
	return filepath.Join(l.ClientDir(), id+constants.FileClientJar)
}

func (l *Layout) ServerJar(id string) string {
	return filepath.Join(l.ServerDir(), id+constants.FileClientJar)
}

func (l *Layout) LibraryPath(artifactPath string) string {
	return filepath.Join(l.LibrariesDir(), filepath.FromSlash(artifactPath))
}

func (l *Layout) AssetIndexPath(id string) string {
	return filepath.Join(l.AssetIndexesDir(), id+constants.FileVersionJSON)
}

func (l *Layout) AssetObjectPath(hash string) string {
	if len(hash) < 2 {
		return filepath.Join(l.AssetObjectsDir(), hash)
	}
	return filepath.Join(l.AssetObjectsDir(), hash[:2], hash)
}

func (l *Layout) Exists(path string) bool {
	fsys, err := l.FS()
	if err != nil {
		return false
	}
	return fsys.Exists(path)
}

// Path resolves a root-relative slash path to the backend path used by FS
// operations (absolute for local backends, still absolute-looking for object
// backends since keys are derived from it).
func (l *Layout) Path(rel string) string {
	return filepath.Join(l.Root, filepath.FromSlash(rel))
}

// RelPath converts an absolute or already-relative path to a canonical
// slash-separated path relative to Root. ok is false for paths outside Root.
func (l *Layout) RelPath(p string) (string, bool) {
	p = filepath.Clean(p)
	var rel string
	if filepath.IsAbs(p) {
		r, err := filepath.Rel(l.Root, p)
		if err != nil {
			return "", false
		}
		rel = r
	} else {
		rel = p
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func WriteJSON(layout *Layout, path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteBytes(layout, path, data)
}

func WriteBytes(layout *Layout, path string, data []byte) error {
	fsys, err := layout.FS()
	if err != nil {
		return err
	}
	return fsys.WriteFile(path, data, safepath.FilePerm)
}

func WriteJSONPath(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteBytesPath(path, data)
}

func WriteBytesPath(path string, data []byte) error {
	dir, name := filepath.Dir(path), filepath.Base(path)
	if name == "" {
		return fmt.Errorf("invalid path %q", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	if err := root.MkdirAll(".", safepath.DirPerm); err != nil && !os.IsExist(err) {
		return err
	}
	return root.WriteFile(name, data, safepath.FilePerm)
}

// CleanStaleParts removes *.part download leftovers older than olderThan and
// returns how many files were removed. Fresh partials are left alone so a
// concurrent archiver keeps its in-flight downloads.
func (l *Layout) CleanStaleParts(olderThan time.Duration) int {
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	_ = filepath.WalkDir(l.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".part") {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err == nil { //#nosec G122 -- maintenance sweep inside our own data dir
			removed++
		}
		return nil
	})
	return removed
}
