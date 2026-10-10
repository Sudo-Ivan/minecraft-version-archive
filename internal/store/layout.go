package store

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/safepath"
)

type Layout struct {
	Root string
	dir  *safepath.Dir
	once sync.Once
	err  error
}

func New(root string) *Layout {
	return &Layout{Root: filepath.Clean(root)}
}

func (l *Layout) Dir() (*safepath.Dir, error) {
	l.once.Do(func() {
		l.dir, l.err = safepath.OpenDir(l.Root)
	})
	return l.dir, l.err
}

func (l *Layout) ReadFile(abs string) ([]byte, error) {
	d, err := l.Dir()
	if err != nil {
		return nil, err
	}
	return d.ReadFile(abs)
}

func (l *Layout) OpenFile(abs string) (*os.File, error) {
	d, err := l.Dir()
	if err != nil {
		return nil, err
	}
	return d.OpenFile(abs)
}

func (l *Layout) Stat(abs string) (fs.FileInfo, error) {
	d, err := l.Dir()
	if err != nil {
		return nil, err
	}
	return d.Stat(abs)
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
	dir, err := l.Dir()
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if err := dir.MkdirAll(d, safepath.DirPerm); err != nil {
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
	d, err := l.Dir()
	if err != nil {
		return false
	}
	return d.Exists(path)
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
	d, err := layout.Dir()
	if err != nil {
		return err
	}
	return d.WriteFile(path, data, safepath.FilePerm)
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
