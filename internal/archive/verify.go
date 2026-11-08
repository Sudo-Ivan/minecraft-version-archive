package archive

import (
	"context"

	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/store"
)

func (a *Archiver) ensureArtifact(ctx context.Context, url, dest string, spec integrity.Spec, peerFn func() bool, prog *Progress) error {
	if a.resolveFromIndex(dest, spec, prog) {
		return nil
	}

	fsys, err := a.layout.FS()
	if err != nil {
		return err
	}

	if a.layout.Exists(dest) {
		if err := integrity.VerifyFile(fsys, dest, spec); err == nil {
			a.index.Record(spec, dest)
			prog.TaskDone(a.fileSize(dest))
			return nil
		}
		_ = a.layout.Remove(dest)
	}

	if peerFn != nil && peerFn() {
		if err := integrity.VerifyFile(fsys, dest, spec); err != nil {
			_ = a.layout.Remove(dest)
		} else {
			a.index.Record(spec, dest)
			prog.TaskDone(a.fileSize(dest))
			return nil
		}
	}

	if err := a.fetch.DownloadVerified(ctx, url, dest, spec); err != nil {
		return err
	}
	a.index.Record(spec, dest)
	prog.TaskDone(a.fileSize(dest))
	return nil
}

func (a *Archiver) resolveFromIndex(dest string, spec integrity.Spec, prog *Progress) bool {
	if a.index == nil || !spec.Known() {
		return false
	}
	src, ok := a.index.Lookup(spec)
	if !ok {
		return false
	}
	if err := store.LinkOrCopy(a.layout, src, dest); err != nil {
		return false
	}
	fsys, err := a.layout.FS()
	if err != nil {
		return false
	}
	if err := integrity.VerifyFile(fsys, dest, spec); err != nil {
		_ = a.layout.Remove(dest)
		return false
	}
	a.index.Record(spec, dest)
	prog.TaskDone(a.fileSize(dest))
	return true
}
