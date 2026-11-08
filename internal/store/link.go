package store

import (
	"io"
	"os"
	"path/filepath"

	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/vfs"
)

func LinkOrCopy(layout *Layout, src, dest string) error {
	if samePath(src, dest) {
		return nil
	}
	fsys, err := layout.FS()
	if err != nil {
		return err
	}
	if err := fsys.MkdirAll(filepath.Dir(dest), safepath.DirPerm); err != nil {
		return err
	}
	_ = fsys.Remove(dest)

	// Backends with a native copy (e.g. S3 CopyObject) dedupe server-side.
	if copier, ok := fsys.(vfs.Copier); ok {
		if err := copier.Copy(src, dest); err == nil {
			return nil
		}
	}

	// Hard links only exist on local filesystems; remote backends dedupe via
	// Copier above or stream a copy below.
	if remote, ok := fsys.(vfs.RemoteFS); !ok || !remote.IsRemote() {
		if err := os.Link(src, dest); err == nil {
			return nil
		}
	}
	return copyFile(fsys, src, dest)
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return aa == bb
}

func copyFile(fsys vfs.FS, src, dest string) error {
	in, err := fsys.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := fsys.OpenFileFlags(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, safepath.FilePerm)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}
