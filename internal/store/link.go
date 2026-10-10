package store

import (
	"io"
	"os"
	"path/filepath"

	"minecraft-version-archive/internal/safepath"
)

func LinkOrCopy(layout *Layout, src, dest string) error {
	if samePath(src, dest) {
		return nil
	}
	dir, err := layout.Dir()
	if err != nil {
		return err
	}
	if err := dir.MkdirAll(filepath.Dir(dest), safepath.DirPerm); err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Link(src, dest); err == nil {
		return nil
	}
	return copyFile(src, dest, dir)
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return aa == bb
}

func copyFile(src, dest string, dir *safepath.Dir) error {
	in, err := dir.OpenFile(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := dir.OpenFileFlags(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, safepath.FilePerm)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}
