package safepath

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"minecraft-version-archive/internal/vfs"
)

type Dir struct {
	path string
	root *os.Root
}

func OpenDir(path string) (*Dir, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(path, DirPerm); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &Dir{path: path, root: root}, nil
}

func (d *Dir) Path() string {
	return d.path
}

func (d *Dir) rel(abs string) (string, error) {
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(d.path, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q outside %q", abs, d.path)
	}
	return rel, nil
}

func (d *Dir) ReadFile(abs string) ([]byte, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	return d.root.ReadFile(rel)
}

func (d *Dir) WriteFile(abs string, data []byte, perm fs.FileMode) error {
	rel, err := d.rel(abs)
	if err != nil {
		return err
	}
	if err := d.root.MkdirAll(filepath.Dir(rel), DirPerm); err != nil && !os.IsExist(err) {
		return err
	}
	return d.root.WriteFile(rel, data, perm)
}

func (d *Dir) Open(abs string) (vfs.File, error) {
	return d.OpenFile(abs)
}

func (d *Dir) OpenFile(abs string) (vfs.File, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	return d.root.Open(rel)
}

func (d *Dir) Create(abs string) (vfs.File, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	if err := d.root.MkdirAll(filepath.Dir(rel), DirPerm); err != nil && !os.IsExist(err) {
		return nil, err
	}
	return d.root.OpenFile(rel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FilePerm)
}

func (d *Dir) OpenFileFlags(abs string, flag int, perm fs.FileMode) (vfs.File, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	if flag&(os.O_CREATE|os.O_WRONLY|os.O_RDWR) != 0 {
		if err := d.root.MkdirAll(filepath.Dir(rel), DirPerm); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	return d.root.OpenFile(rel, flag, perm)
}

func (d *Dir) MkdirAll(abs string, perm fs.FileMode) error {
	rel, err := d.rel(abs)
	if err != nil {
		return err
	}
	return d.root.MkdirAll(rel, perm)
}

func (d *Dir) Stat(abs string) (fs.FileInfo, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	return d.root.Stat(rel)
}

func (d *Dir) Exists(abs string) bool {
	_, err := d.Stat(abs)
	return err == nil
}

func (d *Dir) Remove(abs string) error {
	rel, err := d.rel(abs)
	if err != nil {
		return err
	}
	return d.root.Remove(rel)
}

// RemoveAll deletes abs and everything beneath it. When abs is the root
// itself only its contents are removed, since deleting the root would
// invalidate the open Root handle.
func (d *Dir) RemoveAll(abs string) error {
	rel, err := d.rel(abs)
	if err != nil {
		return err
	}
	if rel == "." {
		entries, err := d.readDirRel(rel)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := d.root.RemoveAll(e.Name()); err != nil {
				return err
			}
		}
		return nil
	}
	return d.root.RemoveAll(rel)
}

func (d *Dir) Rename(src, dst string) error {
	srcRel, err := d.rel(src)
	if err != nil {
		return err
	}
	dstRel, err := d.rel(dst)
	if err != nil {
		return err
	}
	if err := d.root.MkdirAll(filepath.Dir(dstRel), DirPerm); err != nil && !os.IsExist(err) {
		return err
	}
	return d.root.Rename(srcRel, dstRel)
}

func (d *Dir) WriteFileExclusive(abs string, data []byte, perm fs.FileMode) error {
	rel, err := d.rel(abs)
	if err != nil {
		return err
	}
	if err := d.root.MkdirAll(filepath.Dir(rel), DirPerm); err != nil && !os.IsExist(err) {
		return err
	}
	f, err := d.root.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (d *Dir) ReadDir(abs string) ([]vfs.DirEntry, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	return d.readDirRel(rel)
}

func (d *Dir) readDirRel(rel string) ([]vfs.DirEntry, error) {
	f, err := d.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]vfs.DirEntry, len(entries))
	for i, e := range entries {
		out[i] = e
	}
	return out, nil
}

type statEntry struct {
	name string
	info fs.FileInfo
}

func (e statEntry) Name() string               { return e.name }
func (e statEntry) IsDir() bool                { return e.info.IsDir() }
func (e statEntry) Info() (fs.FileInfo, error) { return e.info, nil }

func joinRel(rel, name string) string {
	if rel == "." || rel == "" {
		return name
	}
	return filepath.Join(rel, name)
}

func (d *Dir) Walk(abs string, fn vfs.WalkFunc) error {
	rel, err := d.rel(abs)
	if err != nil {
		return err
	}
	info, err := d.root.Stat(rel)
	if err != nil {
		return fn(abs, nil, err)
	}
	if err := fn(abs, statEntry{filepath.Base(abs), info}, nil); err != nil {
		if err == fs.SkipDir {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return d.walkChildren(rel, abs, fn)
}

func (d *Dir) walkChildren(rel, abs string, fn vfs.WalkFunc) error {
	entries, err := d.readDirRel(rel)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := d.walkChild(joinRel(rel, e.Name()), filepath.Join(abs, e.Name()), e, fn); err != nil {
			if err == fs.SkipDir {
				return nil
			}
			return err
		}
	}
	return nil
}

func (d *Dir) walkChild(rel, abs string, e vfs.DirEntry, fn vfs.WalkFunc) error {
	ferr := fn(abs, e, nil)
	if ferr != nil {
		if ferr == fs.SkipDir {
			if e.IsDir() {
				return nil
			}
			return fs.SkipDir
		}
		return ferr
	}
	if !e.IsDir() {
		return nil
	}
	entries, err := d.readDirRel(rel)
	if err != nil {
		if rerr := fn(abs, e, err); rerr != nil && rerr != fs.SkipDir {
			return rerr
		}
		return nil
	}
	for _, ce := range entries {
		if err := d.walkChild(joinRel(rel, ce.Name()), filepath.Join(abs, ce.Name()), ce, fn); err != nil {
			if err == fs.SkipDir {
				return nil
			}
			return err
		}
	}
	return nil
}

func (d *Dir) CopyFromReader(abs string, r io.Reader, limit int64) (int64, error) {
	out, err := d.Create(abs)
	if err != nil {
		return 0, err
	}
	defer func() { _ = out.Close() }()
	if limit > 0 {
		r = io.LimitReader(r, limit)
	}
	return io.Copy(out, r)
}

func ReadFileAt(path string) ([]byte, error) {
	path = filepath.Clean(path)
	dir, name := filepath.Dir(path), filepath.Base(path)
	if name == "" || name == "." {
		return nil, fmt.Errorf("invalid path %q", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return root.ReadFile(name)
}

func OpenFileAt(path string) (*os.File, error) {
	path = filepath.Clean(path)
	dir, name := filepath.Dir(path), filepath.Base(path)
	if name == "" || name == "." {
		return nil, fmt.Errorf("invalid path %q", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return root.Open(name)
}
