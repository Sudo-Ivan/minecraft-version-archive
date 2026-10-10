package safepath

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

func (d *Dir) OpenFile(abs string) (*os.File, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	return d.root.Open(rel)
}

func (d *Dir) Create(abs string) (*os.File, error) {
	rel, err := d.rel(abs)
	if err != nil {
		return nil, err
	}
	if err := d.root.MkdirAll(filepath.Dir(rel), DirPerm); err != nil && !os.IsExist(err) {
		return nil, err
	}
	return d.root.OpenFile(rel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FilePerm)
}

func (d *Dir) OpenFileFlags(abs string, flag int, perm fs.FileMode) (*os.File, error) {
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
