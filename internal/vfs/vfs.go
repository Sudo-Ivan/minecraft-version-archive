// Package vfs defines the virtual filesystem abstraction used for the
// archive data directory. The local backend stores files under data_dir;
// object-store backends (S3) map the same paths onto object keys.
package vfs

import (
	"io"
	"io/fs"
)

// File is a handle to an open file. It supports streaming reads, random
// access reads, seeking, and (for writable handles) writing.
type File interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Writer
	io.Closer
	Stat() (fs.FileInfo, error)
}

// DirEntry describes one entry returned by ReadDir or Walk.
type DirEntry interface {
	Name() string
	IsDir() bool
	Info() (fs.FileInfo, error)
}

// WalkFunc is called for every entry visited by FS.Walk. Returning
// fs.SkipDir on a directory skips its contents; returning fs.SkipDir on a
// file skips the remaining entries of its containing directory, matching
// filepath.WalkDir semantics.
type WalkFunc func(path string, d DirEntry, err error) error

// FS is the storage backend for the data directory. All paths are
// filesystem-style absolute paths rooted at the layout root; backends map
// them to their native addressing (local paths or object keys).
type FS interface {
	// ReadFile reads the whole file at path.
	ReadFile(path string) ([]byte, error)
	// Open opens path for reading.
	Open(path string) (File, error)
	// Create opens path for writing, truncating any existing content.
	Create(path string) (File, error)
	// OpenFileFlags opens path with the given os.OpenFile-style flags.
	OpenFileFlags(path string, flag int, perm fs.FileMode) (File, error)
	// WriteFile writes data to path, replacing existing content.
	WriteFile(path string, data []byte, perm fs.FileMode) error
	// WriteFileExclusive writes data to path only when it does not already
	// exist, failing with an error wrapping fs.ErrExist otherwise.
	WriteFileExclusive(path string, data []byte, perm fs.FileMode) error
	// MkdirAll creates path and any missing parents. Object-store backends
	// may treat this as a no-op.
	MkdirAll(path string, perm fs.FileMode) error
	// Stat returns file info for path.
	Stat(path string) (fs.FileInfo, error)
	// Exists reports whether path exists.
	Exists(path string) bool
	// Remove deletes the file at path. Missing files are not an error.
	Remove(path string) error
	// Rename moves src to dst, replacing dst when supported.
	Rename(src, dst string) error
	// ReadDir lists the direct children of the directory at path.
	ReadDir(path string) ([]DirEntry, error)
	// Walk visits every entry under root depth-first.
	Walk(root string, fn WalkFunc) error
	// CopyFromReader streams r into path, creating parent directories.
	CopyFromReader(path string, r io.Reader, limit int64) (int64, error)
}

// Digester is an optional FS capability: stored content checksums that let
// integrity verification run without reading the payload.
type Digester interface {
	// Digest returns the stored sha1/sha256 hex digests and size for path.
	// ok is false when no checksums are stored for the object.
	Digest(path string) (sha1, sha256 string, size int64, ok bool)
}

// Copier is an optional FS capability: an efficient backend-side copy.
type Copier interface {
	Copy(src, dst string) error
}

// Purger is an optional FS capability: recursive delete of a path and
// everything under it.
type Purger interface {
	RemoveAll(path string) error
}

// Redirector is an optional FS capability: a temporary URL that serves the
// file directly (e.g. a presigned object URL), letting the server hand
// bandwidth off to the backend.
type Redirector interface {
	PresignGet(path string, ttlSec int64) (string, error)
}

// KeySeter is an optional FS capability: the set of all file paths present
// in the backend, expressed as slash-separated paths relative to the FS
// root. Backends that can answer this with a single listing (object stores)
// avoid per-entry stat calls.
type KeySeter interface {
	KeySet() (map[string]struct{}, error)
}

// RemoteFS marks backends whose writes are remote/network operations.
// Callers use it to choose synchronous vs. deferred persistence strategies.
type RemoteFS interface {
	IsRemote() bool
}

// DigestInfo may be implemented by fs.FileInfo values returned from
// ReadDir/Walk entries when the backend already knows stored checksums
// (e.g. object listings that include user metadata).
type DigestInfo interface {
	StoredDigest() (sha1, sha256 string, ok bool)
}
