package store

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/mojang"
	"minecraft-version-archive/internal/safepath"
)

const (
	// Mojang client/server jars and libraries are under ~200 MiB each; 1 GiB leaves large headroom.
	maxArchiveZipEntryBytes = 1 << 30

	// Inventory indexes and other non-game payloads.
	maxMiscZipEntryBytes = 64 << 20
)

var archiveZipPrefixes = []string{
	constants.DirManifest,
	constants.DirVersions,
	constants.DirJars,
	constants.DirLibraries,
	constants.DirAssets,
	constants.DirFabric,
	constants.DirJava,
}

func zipEntryByteLimit(relName string) int64 {
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relName)))
	for _, prefix := range archiveZipPrefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return maxArchiveZipEntryBytes
		}
	}
	return maxMiscZipEntryBytes
}

var exportSkipPrefixes = []string{
	filepath.Join(constants.DirCoordinator, constants.DirClaims),
}

type ExportOptions struct {
	Versions []string
}

type ZipResult struct {
	Files int64
	Bytes int64
}

func ExportZip(src, dest string, opts ExportOptions) (ZipResult, error) {
	src = filepath.Clean(src)
	info, err := os.Stat(src)
	if err != nil {
		return ZipResult{}, err
	}
	if !info.IsDir() {
		return ZipResult{}, fmt.Errorf("export source must be a directory")
	}

	layout := New(src)
	srcDir, err := layout.Dir()
	if err != nil {
		return ZipResult{}, err
	}

	var resolved []string
	var allowPaths map[string]struct{}
	var filteredManifest []byte

	if len(opts.Versions) > 0 {
		manifestData, err := layout.ReadFile(layout.ManifestPath())
		if err != nil {
			return ZipResult{}, fmt.Errorf("read manifest: %w", err)
		}
		var manifest mojang.Manifest
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			return ZipResult{}, fmt.Errorf("parse manifest: %w", err)
		}
		resolved, err = mojang.ResolveVersionIDs(opts.Versions, manifest.Versions)
		if err != nil {
			return ZipResult{}, err
		}
		allowPaths, err = CollectExportPaths(layout, resolved)
		if err != nil {
			return ZipResult{}, err
		}
		filteredManifest, err = FilterManifestVersions(layout, layout.ManifestPath(), resolved)
		if err != nil {
			return ZipResult{}, err
		}
	}

	destDir, destName := filepath.Dir(dest), filepath.Base(dest)
	destRoot, err := os.OpenRoot(destDir)
	if err != nil {
		return ZipResult{}, err
	}
	of, err := destRoot.OpenFile(destName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, safepath.FilePerm)
	if err != nil {
		return ZipResult{}, err
	}
	defer of.Close()

	w := zip.NewWriter(of)
	defer w.Close()

	var result ZipResult
	if filteredManifest != nil {
		n, err := writeZipBytes(w, constants.DirManifest+"/"+constants.FileManifest, filteredManifest)
		if err != nil {
			return ZipResult{}, err
		}
		result.Files++
		result.Bytes += n
	}

	if allowPaths != nil {
		for rel := range allowPaths {
			abs := filepath.Join(src, filepath.FromSlash(rel))
			n, err := writeZipFile(srcDir, w, abs, rel)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return ZipResult{}, err
			}
			result.Files++
			result.Bytes += n
		}
		return result, nil
	}

	err = filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if shouldSkipExport(relSlash) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		n, err := writeZipFile(srcDir, w, path, relSlash)
		if err != nil {
			return err
		}
		result.Files++
		result.Bytes += n
		return nil
	})
	if err != nil {
		return ZipResult{}, err
	}
	return result, nil
}

func writeZipFile(dir *safepath.Dir, w *zip.Writer, path, rel string) (int64, error) {
	info, err := dir.Stat(path)
	if err != nil {
		return 0, err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return 0, err
	}
	header.Name = rel
	header.Method = zip.Store
	entry, err := w.CreateHeader(header)
	if err != nil {
		return 0, err
	}
	in, err := dir.OpenFile(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }()
	n, err := io.Copy(entry, in)
	return n, err
}

func writeZipBytes(w *zip.Writer, name string, data []byte) (int64, error) {
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Store,
	}
	entry, err := w.CreateHeader(header)
	if err != nil {
		return 0, err
	}
	n, err := entry.Write(data)
	return int64(n), err
}

func ImportZip(dest, zipPath string, replace bool) (ZipResult, error) {
	dest = filepath.Clean(dest)
	if replace {
		if err := os.RemoveAll(dest); err != nil {
			return ZipResult{}, err
		}
	}
	layout := New(dest)
	if err := layout.Ensure(); err != nil {
		return ZipResult{}, err
	}
	destDir, err := layout.Dir()
	if err != nil {
		return ZipResult{}, err
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return ZipResult{}, err
	}
	defer r.Close()

	var result ZipResult
	for _, file := range r.File {
		target, err := safeZipTarget(dest, file.Name)
		if err != nil {
			return ZipResult{}, err
		}
		if file.FileInfo().IsDir() {
			if err := destDir.MkdirAll(target, safepath.DirPerm); err != nil {
				return ZipResult{}, err
			}
			continue
		}
		if err := destDir.MkdirAll(filepath.Dir(target), safepath.DirPerm); err != nil {
			return ZipResult{}, err
		}
		limit := zipEntryByteLimit(file.Name)
		if declared, ok := zipDeclaredSize(file.UncompressedSize64); ok {
			if declared > limit {
				return ZipResult{}, fmt.Errorf("zip entry %q exceeds size limit (%d bytes)", file.Name, limit)
			}
		}
		in, err := file.Open()
		if err != nil {
			return ZipResult{}, err
		}
		perm := file.Mode().Perm()
		if perm&0o077 != 0 {
			perm = safepath.FilePerm
		}
		out, err := destDir.OpenFileFlags(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
		if err != nil {
			_ = in.Close()
			return ZipResult{}, err
		}
		n, err := copyZipEntry(out, in, limit)
		_ = in.Close()
		_ = out.Close()
		if err != nil {
			_ = os.Remove(target)
			return ZipResult{}, err
		}
		if declared, ok := zipDeclaredSize(file.UncompressedSize64); ok && n != declared {
			_ = os.Remove(target)
			return ZipResult{}, fmt.Errorf("zip entry %q size mismatch", file.Name)
		}
		result.Files++
		result.Bytes += n
	}
	return result, nil
}

func copyZipEntry(out io.Writer, in io.Reader, limit int64) (int64, error) {
	n, err := io.Copy(out, io.LimitReader(in, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("entry exceeds %d byte limit", limit)
	}
	return n, nil
}

func zipDeclaredSize(sz uint64) (int64, bool) {
	if sz == 0 || sz > uint64(math.MaxInt64) {
		return 0, false
	}
	return int64(sz), true
}

func shouldSkipExport(rel string) bool {
	for _, prefix := range exportSkipPrefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}

func safeZipTarget(destRoot, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid zip entry %q", name)
	}
	target := filepath.Join(destRoot, clean)
	rel, err := filepath.Rel(destRoot, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid zip entry %q", name)
	}
	return target, nil
}
