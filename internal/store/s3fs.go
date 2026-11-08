package store

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/vfs"
)

// s3Client is the subset of *minio.Client used by s3fs. It exists so tests
// can substitute an in-memory fake.
type s3Client interface {
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (objectReader, error)
	StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error
	RemoveObjects(ctx context.Context, bucketName string, objectsCh <-chan minio.ObjectInfo, opts minio.RemoveObjectsOptions) <-chan minio.RemoveObjectError
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	CopyObject(ctx context.Context, dst minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error)
	PresignedGetObject(ctx context.Context, bucketName, objectName string, expires time.Duration, reqParams url.Values) (*url.URL, error)
	BucketExists(ctx context.Context, bucketName string) (bool, error)
}

// objectReader is the read side of a fetched object.
type objectReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
	Stat() (minio.ObjectInfo, error)
}

// s3fs maps the data directory onto objects in an S3-compatible bucket.
// Logical paths under Root become object keys under bucket/prefix. Files
// ending in .part (in-flight downloads) and the .spool scratch area stay on
// the local disk so resumable downloads and streaming uploads keep working.
type s3fs struct {
	cli         s3Client
	bucket      string
	prefix      string // normalized, no leading/trailing slash; "" = bucket root
	root        string // absolute local path; also the scratch dir
	local       *safepath.Dir
	redirect    bool
	redirectTTL time.Duration
	ctx         context.Context
	spoolSeq    atomic.Uint64
}

var _ vfs.FS = (*s3fs)(nil)
var _ vfs.Digester = (*s3fs)(nil)
var _ vfs.Copier = (*s3fs)(nil)
var _ vfs.Purger = (*s3fs)(nil)
var _ vfs.Redirector = (*s3fs)(nil)
var _ vfs.KeySeter = (*s3fs)(nil)
var _ vfs.RemoteFS = (*s3fs)(nil)

func newS3FS(root string, opts S3Options) (*s3fs, error) {
	if strings.TrimSpace(opts.Bucket) == "" {
		return nil, fmt.Errorf("s3 backend requires a bucket name")
	}
	endpoint, secure := resolveS3Endpoint(opts)
	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
	}

	var creds *credentials.Credentials
	switch {
	case opts.Anonymous:
		creds = credentials.NewStaticV4("", "", "")
	case opts.AccessKey != "" || opts.SecretKey != "":
		creds = credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, opts.SessionToken)
	default:
		// AWS-style chain: env vars, shared credentials file, instance
		// metadata. Falls back to anonymous so public buckets work with
		// zero configuration.
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{},
			&credentials.EnvMinio{},
			&credentials.FileAWSCredentials{},
			&credentials.IAM{},
		})
	}

	lookup := minio.BucketLookupAuto
	if opts.PathStyle {
		lookup = minio.BucketLookupPath
	}

	cli, err := minio.New(endpoint, &minio.Options{
		Creds:        creds,
		Secure:       secure,
		Region:       opts.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}

	local, err := safepath.OpenDir(filepath.Clean(root))
	if err != nil {
		return nil, err
	}

	prefix := normalizeS3Prefix(opts.Prefix)
	ttl := opts.RedirectTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &s3fs{
		cli:         minioClient{cli},
		bucket:      opts.Bucket,
		prefix:      prefix,
		root:        filepath.Clean(root),
		local:       local,
		redirect:    opts.Redirect,
		redirectTTL: ttl,
		ctx:         context.Background(),
	}, nil
}

// minioClient wraps *minio.Client to satisfy s3Client: GetObject must return
// the objectReader interface rather than the concrete *minio.Object.
type minioClient struct{ *minio.Client }

func (m minioClient) GetObject(ctx context.Context, bucket, object string, opts minio.GetObjectOptions) (objectReader, error) {
	return m.Client.GetObject(ctx, bucket, object, opts)
}

func normalizeS3Prefix(p string) string {
	p = strings.Trim(p, "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

func resolveS3Endpoint(opts S3Options) (endpoint string, secure bool) {
	ep := strings.TrimSpace(opts.Endpoint)
	// Only http(s) schemes are honored; "host:port" must not be mistaken for
	// a scheme (url.Parse would read "localhost" as one).
	if u, err := url.Parse(ep); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		host := u.Host
		if host == "" {
			host = u.Path
		}
		return strings.TrimSuffix(host, "/"), u.Scheme == "https"
	}
	ep = strings.TrimSuffix(ep, "/")
	if opts.SecureAuto {
		return ep, !isLocalS3Host(ep)
	}
	return ep, opts.Secure
}

func isLocalS3Host(endpoint string) bool {
	host := endpoint
	if h, _, err := net.SplitHostPort(endpoint); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

// relOf maps an absolute path under root to a slash-separated relative path.
func (s *s3fs) relOf(abs string) (string, error) {
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(s.root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q outside %q", abs, s.root)
	}
	return rel, nil
}

// isLocalRel reports whether a relative path stays on local disk: in-flight
// .part downloads and the .spool upload staging area.
func isLocalRel(rel string) bool {
	relSlash := filepath.ToSlash(rel)
	return strings.HasSuffix(relSlash, ".part") || strings.HasPrefix(relSlash, ".spool/")
}

// keyOfRel maps a relative path to its object key. rel may be "." for the
// root, which maps to the configured prefix itself.
func (s *s3fs) keyOfRel(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" || rel == "/" {
		return s.prefix
	}
	if s.prefix == "" {
		return rel
	}
	return s.prefix + "/" + rel
}

// listPrefix returns the key prefix used to list the children of rel.
func (s *s3fs) listPrefix(rel string) string {
	key := s.keyOfRel(rel)
	if key == "" {
		return ""
	}
	return key + "/"
}

// absOfKey maps an object key back to an absolute path under root.
func (s *s3fs) absOfKey(key string) string {
	rel := key
	if s.prefix != "" {
		if rel == s.prefix {
			rel = "."
		} else {
			rel = strings.TrimPrefix(key, s.prefix+"/")
		}
	}
	if rel == "" {
		rel = "."
	}
	return filepath.Join(s.root, filepath.FromSlash(rel))
}

func (s *s3fs) keyOf(abs string) (string, string, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return "", "", err
	}
	return rel, s.keyOfRel(rel), nil
}

func normS3Err(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
		return err
	}
	rerr := minio.ToErrorResponse(err)
	switch rerr.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound", "NoSuchUpload", "InvalidBucketName":
		return fmt.Errorf("%w: %s", fs.ErrNotExist, rerr.Message)
	}
	return err
}

func isNotExistErr(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist)
}

func isExistErr(err error) bool {
	return errors.Is(err, fs.ErrExist) || errors.Is(err, os.ErrExist)
}

func (s *s3fs) ReadFile(abs string) ([]byte, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return nil, err
	}
	if isLocalRel(rel) {
		return s.local.ReadFile(abs)
	}
	obj, err := s.cli.GetObject(s.ctx, s.bucket, s.keyOfRel(rel), minio.GetObjectOptions{})
	if err != nil {
		return nil, normS3Err(err)
	}
	defer func() { _ = obj.Close() }()
	if _, err := obj.Stat(); err != nil {
		return nil, normS3Err(err)
	}
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, normS3Err(err)
	}
	return data, nil
}

func (s *s3fs) Open(abs string) (vfs.File, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return nil, err
	}
	if isLocalRel(rel) {
		return s.local.OpenFile(abs)
	}
	obj, err := s.cli.GetObject(s.ctx, s.bucket, s.keyOfRel(rel), minio.GetObjectOptions{})
	if err != nil {
		return nil, normS3Err(err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, normS3Err(err)
	}
	return &s3ReadFile{obj: obj, info: info, name: filepath.Base(abs)}, nil
}

func (s *s3fs) Create(abs string) (vfs.File, error) {
	return s.OpenFileFlags(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, safepath.FilePerm)
}

func (s *s3fs) OpenFileFlags(abs string, flag int, perm fs.FileMode) (vfs.File, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return nil, err
	}
	if isLocalRel(rel) {
		return s.local.OpenFileFlags(abs, flag, perm)
	}
	if flag&os.O_APPEND != 0 || flag&os.O_RDWR != 0 {
		return nil, fmt.Errorf("s3 backend does not support append/read-write file handles: %s", abs)
	}
	if flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0 {
		return s.Open(abs)
	}
	return s.newWriteFile(abs, rel)
}

// newWriteFile stages content in a local spool file and uploads it to the
// bucket when the handle is closed.
func (s *s3fs) newWriteFile(abs, rel string) (vfs.File, error) {
	seq := s.spoolSeq.Add(1)
	spoolRel := filepath.Join(".spool", filepath.FromSlash(s.keyOfRel(rel))+fmt.Sprintf(".%d.%d.part", os.Getpid(), seq))
	spoolAbs := filepath.Join(s.root, spoolRel)
	tmp, err := s.local.OpenFileFlags(spoolAbs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, safepath.FilePerm)
	if err != nil {
		return nil, err
	}
	return &s3WriteFile{
		s:      s,
		key:    s.keyOfRel(rel),
		name:   filepath.Base(abs),
		tmp:    tmp,
		tmpAbs: spoolAbs,
		h1:     sha1.New(),
		h2:     sha256.New(),
	}, nil
}

func (s *s3fs) WriteFile(abs string, data []byte, perm fs.FileMode) error {
	rel, err := s.relOf(abs)
	if err != nil {
		return err
	}
	if isLocalRel(rel) {
		return s.local.WriteFile(abs, data, perm)
	}
	_, err = s.putObject(abs, bytes.NewReader(data), int64(len(data)), s.checksumMetadata(data))
	return err
}

func (s *s3fs) WriteFileExclusive(abs string, data []byte, perm fs.FileMode) error {
	rel, err := s.relOf(abs)
	if err != nil {
		return err
	}
	if isLocalRel(rel) {
		return s.local.WriteFileExclusive(abs, data, perm)
	}
	key := s.keyOfRel(rel)
	opts := minio.PutObjectOptions{
		ContentType:  contentTypeFor(key),
		UserMetadata: s.checksumMetadata(data),
	}
	opts.SetMatchETagExcept("*") // If-None-Match: * — create only if absent
	_, err = s.cli.PutObject(s.ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), opts)
	if err == nil {
		return nil
	}
	if isPreconditionFailed(err) {
		return fmt.Errorf("%w: %s", fs.ErrExist, key)
	}
	// Server may not support conditional writes; verify existence instead.
	if _, serr := s.cli.StatObject(s.ctx, s.bucket, key, minio.StatObjectOptions{}); serr == nil {
		return fmt.Errorf("%w: %s", fs.ErrExist, key)
	}
	// Retry without the condition (best effort on non-supporting backends).
	_, err = s.putObject(abs, bytes.NewReader(data), int64(len(data)), s.checksumMetadata(data))
	return err
}

func (s *s3fs) MkdirAll(abs string, perm fs.FileMode) error {
	rel, err := s.relOf(abs)
	if err != nil {
		return err
	}
	if isLocalRel(rel) {
		return s.local.MkdirAll(abs, perm)
	}
	// Object stores have no real directories; nothing to do.
	return nil
}

func (s *s3fs) Stat(abs string) (fs.FileInfo, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return nil, err
	}
	if isLocalRel(rel) {
		return s.local.Stat(abs)
	}
	info, err := s.statKey(s.keyOfRel(rel))
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (s *s3fs) statKey(key string) (fs.FileInfo, error) {
	if key == "" {
		return s3FileInfo{name: filepath.Base(s.root), isDir: true}, nil
	}
	info, err := s.cli.StatObject(s.ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return s3InfoFromObject(info), nil
	}
	if !isNotExistErr(normS3Err(err)) {
		return nil, normS3Err(err)
	}
	// Maybe a "directory": any object under key+"/" means it exists.
	ch := s.cli.ListObjects(s.ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    key + "/",
		Recursive: false,
		MaxKeys:   1,
	})
	for oi := range ch {
		if oi.Err != nil {
			return nil, normS3Err(oi.Err)
		}
		return s3DirInfo(s.absOfKey(key)), nil
	}
	return nil, fmt.Errorf("%w: %s", fs.ErrNotExist, key)
}

func (s *s3fs) Exists(abs string) bool {
	_, err := s.Stat(abs)
	return err == nil
}

func (s *s3fs) Remove(abs string) error {
	rel, err := s.relOf(abs)
	if err != nil {
		return err
	}
	if isLocalRel(rel) {
		return s.local.Remove(abs)
	}
	err = s.cli.RemoveObject(s.ctx, s.bucket, s.keyOfRel(rel), minio.RemoveObjectOptions{})
	if err != nil && !isNotExistErr(normS3Err(err)) {
		return normS3Err(err)
	}
	return nil
}

func (s *s3fs) Rename(src, dst string) error {
	srcRel, err := s.relOf(src)
	if err != nil {
		return err
	}
	dstRel, err := s.relOf(dst)
	if err != nil {
		return err
	}
	if srcRel == dstRel {
		return nil
	}
	srcLocal, dstLocal := isLocalRel(srcRel), isLocalRel(dstRel)
	switch {
	case srcLocal && dstLocal:
		return s.local.Rename(src, dst)
	case !srcLocal && !dstLocal:
		_, err := s.cli.CopyObject(s.ctx,
			minio.CopyDestOptions{Bucket: s.bucket, Object: s.keyOfRel(dstRel)},
			minio.CopySrcOptions{Bucket: s.bucket, Object: s.keyOfRel(srcRel)})
		if err != nil {
			return normS3Err(err)
		}
		return s.Remove(src)
	case srcLocal:
		// local -> remote: hash the file first so the upload carries
		// checksum metadata and later integrity checks stay HEAD-only.
		in, err := s.local.OpenFile(src)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		info, err := in.Stat()
		if err != nil {
			return err
		}
		h1, h2 := sha1.New(), sha256.New()
		if _, err := io.Copy(io.MultiWriter(h1, h2), in); err != nil {
			return err
		}
		if _, err := in.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if _, err := s.putObject(dst, in, info.Size(), hashPairMeta(h1, h2)); err != nil {
			return err
		}
		return s.local.Remove(src)
	default:
		// remote -> local: download then remove the object
		in, err := s.Open(src)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		if err := s.local.MkdirAll(filepath.Dir(dst), safepath.DirPerm); err != nil {
			return err
		}
		out, err := s.local.OpenFileFlags(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, safepath.FilePerm)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return s.Remove(src)
	}
}

func (s *s3fs) ReadDir(abs string) ([]vfs.DirEntry, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return nil, err
	}
	if isLocalRel(rel) {
		return s.local.ReadDir(abs)
	}
	prefix := s.listPrefix(rel)
	ch := s.cli.ListObjects(s.ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:       prefix,
		Recursive:    false,
		WithMetadata: true,
	})
	var entries []vfs.DirEntry
	for oi := range ch {
		if oi.Err != nil {
			return nil, normS3Err(oi.Err)
		}
		name := dirEntryName(prefix, oi.Key)
		if name == "" {
			continue
		}
		if strings.HasSuffix(oi.Key, "/") {
			entries = append(entries, s3DirEntry{info: s3DirInfo(s.absOfKey(strings.TrimSuffix(oi.Key, "/")))})
			continue
		}
		entries = append(entries, s3DirEntry{info: s3InfoFromObject(oi)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// dirEntryName returns the base name of a listed key relative to prefix.
func dirEntryName(prefix, key string) string {
	if !strings.HasPrefix(key, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(key, prefix)
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

func (s *s3fs) Walk(root string, fn vfs.WalkFunc) error {
	rel, err := s.relOf(root)
	if err != nil {
		return err
	}
	if isLocalRel(rel) {
		return s.local.Walk(root, fn)
	}
	info, err := s.statKey(s.keyOfRel(rel))
	if err != nil {
		return fn(root, nil, err)
	}
	if err := fn(root, s3WalkEntry{info: info}, nil); err != nil {
		if err == fs.SkipDir {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return s.walkKeyDir(s.keyOfRel(rel), root, fn)
}

// walkKeyDir lists the immediate children of the object prefix key+"/" and
// recurses into subdirectories depth-first.
func (s *s3fs) walkKeyDir(key, abs string, fn vfs.WalkFunc) error {
	prefix := key
	if prefix != "" {
		prefix += "/"
	}
	ch := s.cli.ListObjects(s.ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:       prefix,
		Recursive:    false,
		WithMetadata: true,
	})
	type pending struct {
		key  string
		abs  string
		info fs.FileInfo
	}
	var dirs []pending
	for oi := range ch {
		if oi.Err != nil {
			return normS3Err(oi.Err)
		}
		if strings.HasSuffix(oi.Key, "/") {
			dkey := strings.TrimSuffix(oi.Key, "/")
			dabs := s.absOfKey(dkey)
			dinfo := s3DirInfo(dabs)
			if err := fn(dabs, s3WalkEntry{info: dinfo}, nil); err != nil {
				if err == fs.SkipDir {
					continue
				}
				return err
			}
			dirs = append(dirs, pending{key: dkey, abs: dabs, info: dinfo})
			continue
		}
		fabs := s.absOfKey(oi.Key)
		finfo := s3InfoFromObject(oi)
		if err := fn(fabs, s3WalkEntry{info: finfo}, nil); err != nil {
			if err == fs.SkipDir {
				// SkipDir on a file skips remaining entries of this dir;
				// drain handled by returning early — approximation: stop this level.
				return nil
			}
			return err
		}
	}
	for _, d := range dirs {
		if err := s.walkKeyDir(d.key, d.abs, fn); err != nil {
			return err
		}
	}
	return nil
}

func (s *s3fs) CopyFromReader(abs string, r io.Reader, limit int64) (int64, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return 0, err
	}
	if isLocalRel(rel) {
		return s.local.CopyFromReader(abs, r, limit)
	}
	if limit > 0 {
		r = io.LimitReader(r, limit)
	}
	// Stage through the local spool so we can hash and size the upload.
	w, err := s.newWriteFile(abs, rel)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, r)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func (s *s3fs) Copy(src, dst string) error {
	srcRel, err := s.relOf(src)
	if err != nil {
		return err
	}
	dstRel, err := s.relOf(dst)
	if err != nil {
		return err
	}
	if isLocalRel(srcRel) || isLocalRel(dstRel) {
		return s.Rename(src, dst)
	}
	_, err = s.cli.CopyObject(s.ctx,
		minio.CopyDestOptions{Bucket: s.bucket, Object: s.keyOfRel(dstRel)},
		minio.CopySrcOptions{Bucket: s.bucket, Object: s.keyOfRel(srcRel)})
	return normS3Err(err)
}

func (s *s3fs) RemoveAll(abs string) error {
	rel, err := s.relOf(abs)
	if err != nil {
		return err
	}
	if isLocalRel(rel) {
		return os.RemoveAll(abs) // #nosec G122 -- scoped to our own data dir
	}
	key := s.keyOfRel(rel)
	// Remove the object itself (if a file) plus everything under key+/.
	listPrefix := key
	if listPrefix != "" {
		listPrefix += "/"
	}
	objs := make(chan minio.ObjectInfo, 100)
	go func() {
		defer close(objs)
		if key != "" {
			objs <- minio.ObjectInfo{Key: key}
		}
		for oi := range s.cli.ListObjects(s.ctx, s.bucket, minio.ListObjectsOptions{
			Prefix:    listPrefix,
			Recursive: true,
		}) {
			if oi.Err == nil {
				objs <- oi
			}
		}
	}()
	for res := range s.cli.RemoveObjects(s.ctx, s.bucket, objs, minio.RemoveObjectsOptions{}) {
		if res.Err != nil {
			return normS3Err(res.Err)
		}
	}
	return nil
}

func (s *s3fs) KeySet() (map[string]struct{}, error) {
	set := make(map[string]struct{})
	prefix := s.prefix
	if prefix != "" {
		prefix += "/"
	}
	for oi := range s.cli.ListObjects(s.ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if oi.Err != nil {
			return nil, normS3Err(oi.Err)
		}
		if strings.HasSuffix(oi.Key, "/") {
			continue
		}
		rel := oi.Key
		if s.prefix != "" {
			rel = strings.TrimPrefix(oi.Key, s.prefix+"/")
		}
		if rel != "" {
			set[filepath.ToSlash(rel)] = struct{}{}
		}
	}
	return set, nil
}

func (s *s3fs) IsRemote() bool { return true }

// Digest returns stored checksums from object metadata (written at upload
// time), avoiding a full object download for integrity checks.
func (s *s3fs) Digest(abs string) (sha1hex, sha256hex string, size int64, ok bool) {
	rel, err := s.relOf(abs)
	if err != nil {
		return "", "", 0, false
	}
	if isLocalRel(rel) {
		if info, err := s.local.Stat(abs); err == nil {
			return "", "", info.Size(), true
		}
		return "", "", 0, false
	}
	info, err := s.cli.StatObject(s.ctx, s.bucket, s.keyOfRel(rel), minio.StatObjectOptions{})
	if err != nil {
		return "", "", 0, false
	}
	sha1hex = metaValue(info.Metadata, "x-amz-meta-mva-sha1")
	sha256hex = metaValue(info.Metadata, "x-amz-meta-mva-sha256")
	return sha1hex, sha256hex, info.Size, sha1hex != "" || sha256hex != ""
}

func metaValue(h http.Header, key string) string {
	return strings.TrimSpace(h.Get(key))
}

func (s *s3fs) PresignGet(abs string, ttlSec int64) (string, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return "", err
	}
	if isLocalRel(rel) {
		return "", fmt.Errorf("cannot presign local file %s", abs)
	}
	u, err := s.cli.PresignedGetObject(s.ctx, s.bucket, s.keyOfRel(rel), time.Duration(ttlSec)*time.Second, nil)
	if err != nil {
		return "", normS3Err(err)
	}
	return u.String(), nil
}

// putObject uploads r to the object key for abs, attaching the given
// checksums as user metadata when provided.
func (s *s3fs) putObject(abs string, r io.Reader, size int64, meta map[string]string) (minio.UploadInfo, error) {
	rel, err := s.relOf(abs)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	key := s.keyOfRel(rel)
	opts := minio.PutObjectOptions{
		ContentType:  contentTypeFor(key),
		UserMetadata: meta,
	}
	info, err := s.cli.PutObject(s.ctx, s.bucket, key, r, size, opts)
	return info, normS3Err(err)
}

func (s *s3fs) checksumMetadata(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}
	h1, h2 := sha1.New(), sha256.New()
	_, _ = h1.Write(data)
	_, _ = h2.Write(data)
	return hashPairMeta(h1, h2)
}

func hashPairMeta(h1, h2 hash.Hash) map[string]string {
	return map[string]string{
		"Mva-Sha1":   hex.EncodeToString(h1.Sum(nil)),
		"Mva-Sha256": hex.EncodeToString(h2.Sum(nil)),
	}
}

func contentTypeFor(key string) string {
	switch strings.ToLower(filepath.Ext(key)) {
	case ".json":
		return "application/json"
	case ".gz":
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}

func isPreconditionFailed(err error) bool {
	return minio.ToErrorResponse(err).Code == "PreconditionFailed" ||
		strings.Contains(err.Error(), "PreconditionFailed")
}

// --- file/info types ---

type s3ReadFile struct {
	obj  objectReader
	info minio.ObjectInfo
	name string
}

func (f *s3ReadFile) Read(p []byte) (int, error) { return f.obj.Read(p) }
func (f *s3ReadFile) ReadAt(p []byte, off int64) (int, error) {
	return f.obj.ReadAt(p, off)
}
func (f *s3ReadFile) Seek(off int64, whence int) (int64, error) {
	return f.obj.Seek(off, whence)
}
func (f *s3ReadFile) Close() error { return f.obj.Close() }
func (f *s3ReadFile) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("read-only file")
}
func (f *s3ReadFile) Stat() (fs.FileInfo, error) {
	return s3InfoFromObjectInfo(f.info, f.name), nil
}

// s3WriteFile stages writes locally and uploads on Close.
type s3WriteFile struct {
	s      *s3fs
	key    string
	name   string
	tmp    vfs.File
	tmpAbs string
	h1     hash.Hash
	h2     hash.Hash
	size   int64
	closed bool
}

func (w *s3WriteFile) Write(p []byte) (int, error) {
	if w.closed {
		return 0, os.ErrClosed
	}
	n, err := w.tmp.Write(p)
	if n > 0 {
		_, _ = w.h1.Write(p[:n])
		_, _ = w.h2.Write(p[:n])
		w.size += int64(n)
	}
	return n, err
}

func (w *s3WriteFile) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	_ = w.tmp.Close()
	defer func() {
		_ = os.Remove(w.tmpAbs)
		// Prune emptied spool parent directories, stopping at .spool.
		spoolRoot := filepath.Join(w.s.root, ".spool")
		for dir := filepath.Dir(w.tmpAbs); dir != spoolRoot && strings.HasPrefix(dir, spoolRoot+string(os.PathSeparator)); dir = filepath.Dir(dir) {
			if err := os.Remove(dir); err != nil {
				break
			}
		}
	}()

	// Re-open the spool for upload.
	in, err := w.s.local.OpenFile(w.tmpAbs)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	opts := minio.PutObjectOptions{
		ContentType:  contentTypeFor(w.key),
		UserMetadata: hashPairMeta(w.h1, w.h2),
	}
	_, err = w.s.cli.PutObject(w.s.ctx, w.s.bucket, w.key, in, w.size, opts)
	return normS3Err(err)
}

func (w *s3WriteFile) Read([]byte) (int, error) { return 0, fmt.Errorf("write-only file") }
func (w *s3WriteFile) ReadAt([]byte, int64) (int, error) {
	return 0, fmt.Errorf("write-only file")
}
func (w *s3WriteFile) Seek(int64, int) (int64, error) {
	return 0, fmt.Errorf("write-only file")
}
func (w *s3WriteFile) Stat() (fs.FileInfo, error) {
	return s3FileInfo{name: w.name, size: w.size, modTime: time.Now()}, nil
}

// --- fs.FileInfo / DirEntry implementations ---

type s3FileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
	sha1    string
	sha256  string
	hasMeta bool
}

func (i s3FileInfo) Name() string { return i.name }
func (i s3FileInfo) Size() int64  { return i.size }
func (i s3FileInfo) Mode() fs.FileMode {
	if i.isDir {
		return fs.ModeDir | 0o755
	}
	return 0o444
}
func (i s3FileInfo) ModTime() time.Time { return i.modTime }
func (i s3FileInfo) IsDir() bool        { return i.isDir }
func (i s3FileInfo) Sys() any           { return nil }

func (i s3FileInfo) StoredDigest() (string, string, bool) {
	if !i.hasMeta {
		return "", "", false
	}
	return i.sha1, i.sha256, true
}

func s3InfoFromObject(oi minio.ObjectInfo) fs.FileInfo {
	name := filepath.Base(strings.TrimSuffix(oi.Key, "/"))
	if name == "" || name == "." {
		name = oi.Key
	}
	return s3InfoFromObjectInfo(oi, name)
}

func s3InfoFromObjectInfo(oi minio.ObjectInfo, name string) fs.FileInfo {
	info := s3FileInfo{
		name:    name,
		size:    oi.Size,
		modTime: oi.LastModified,
		sha1:    metaValue(oi.Metadata, "x-amz-meta-mva-sha1"),
		sha256:  metaValue(oi.Metadata, "x-amz-meta-mva-sha256"),
	}
	info.hasMeta = info.sha1 != "" || info.sha256 != ""
	return info
}

func s3DirInfo(abs string) fs.FileInfo {
	return s3FileInfo{name: filepath.Base(abs), isDir: true}
}

type s3DirEntry struct {
	info fs.FileInfo
}

func (e s3DirEntry) Name() string { return e.info.Name() }
func (e s3DirEntry) IsDir() bool  { return e.info.IsDir() }
func (e s3DirEntry) Info() (fs.FileInfo, error) {
	return e.info, nil
}

type s3WalkEntry struct {
	info fs.FileInfo
}

func (e s3WalkEntry) Name() string { return e.info.Name() }
func (e s3WalkEntry) IsDir() bool  { return e.info.IsDir() }
func (e s3WalkEntry) Info() (fs.FileInfo, error) {
	return e.info, nil
}
