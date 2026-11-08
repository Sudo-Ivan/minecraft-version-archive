package store

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minecraft-version-archive/internal/vfs"
)

// fakeS3 is an in-memory s3Client. rejectExisting simulates a backend that
// honors If-None-Match: * on every PutObject (used to exercise the
// WriteFileExclusive failure path); failPut makes PutObject return a generic
// error to exercise the existence-check fallback.
type fakeS3 struct {
	mu             sync.Mutex
	objects        map[string]*fakeObject
	rejectExisting bool
	failPut        bool
}

type fakeObject struct {
	data     []byte
	metadata http.Header
	modTime  time.Time
	etag     string
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: make(map[string]*fakeObject)}
}

func (f *fakeS3) PutObject(_ context.Context, bucket, object string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failPut {
		return minio.UploadInfo{}, fmt.Errorf("simulated put failure")
	}
	if f.rejectExisting {
		if _, ok := f.objects[object]; ok {
			return minio.UploadInfo{}, minio.ErrorResponse{Code: "PreconditionFailed", Message: "precondition failed"}
		}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	if objectSize >= 0 && int64(len(data)) != objectSize {
		return minio.UploadInfo{}, fmt.Errorf("size mismatch: sent %d declared %d", len(data), objectSize)
	}
	md := http.Header{}
	for k, v := range opts.UserMetadata {
		md.Set("X-Amz-Meta-"+k, v)
	}
	sum := sha1.Sum(data) //#nosec G401 -- test fixture
	f.objects[object] = &fakeObject{data: data, metadata: md, modTime: time.Now(), etag: hex.EncodeToString(sum[:])}
	return minio.UploadInfo{ETag: f.objects[object].etag, Size: int64(len(data))}, nil
}

func (f *fakeS3) GetObject(_ context.Context, bucket, object string, _ minio.GetObjectOptions) (objectReader, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[object]
	if !ok {
		return nil, minio.ErrorResponse{Code: "NoSuchKey", Message: "no such key"}
	}
	return &fakeReader{Reader: bytes.NewReader(o.data), obj: o, name: object}, nil
}

type fakeReader struct {
	*bytes.Reader
	obj  *fakeObject
	name string
}

func (r *fakeReader) ReadAt(p []byte, off int64) (int, error) { return r.Reader.ReadAt(p, off) }
func (r *fakeReader) Seek(off int64, whence int) (int64, error) {
	return r.Reader.Seek(off, whence)
}
func (r *fakeReader) Close() error { return nil }
func (r *fakeReader) Stat() (minio.ObjectInfo, error) {
	return minio.ObjectInfo{
		Key:          r.name,
		Size:         int64(len(r.obj.data)),
		LastModified: r.obj.modTime,
		ETag:         r.obj.etag,
		Metadata:     r.obj.metadata,
	}, nil
}

func (f *fakeS3) StatObject(_ context.Context, bucket, object string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[object]
	if !ok {
		return minio.ObjectInfo{}, minio.ErrorResponse{Code: "NoSuchKey", Message: "no such key"}
	}
	return minio.ObjectInfo{
		Key:          object,
		Size:         int64(len(o.data)),
		LastModified: o.modTime,
		ETag:         o.etag,
		Metadata:     o.metadata,
	}, nil
}

func (f *fakeS3) RemoveObject(_ context.Context, bucket, object string, _ minio.RemoveObjectOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, object)
	return nil
}

func (f *fakeS3) RemoveObjects(_ context.Context, bucket string, objectsCh <-chan minio.ObjectInfo, _ minio.RemoveObjectsOptions) <-chan minio.RemoveObjectError {
	out := make(chan minio.RemoveObjectError, 8)
	go func() {
		defer close(out)
		for oi := range objectsCh {
			f.mu.Lock()
			delete(f.objects, oi.Key)
			f.mu.Unlock()
		}
	}()
	return out
}

// ListObjects emulates S3 listing: Recursive=false returns immediate
// children plus common prefixes (keys ending in "/").
func (f *fakeS3) ListObjects(_ context.Context, bucket string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	out := make(chan minio.ObjectInfo, 64)
	go func() {
		defer close(out)
		f.mu.Lock()
		defer f.mu.Unlock()
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, opts.Prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		seenPrefix := map[string]bool{}
		for _, k := range keys {
			if !opts.Recursive {
				rest := strings.TrimPrefix(k, opts.Prefix)
				if i := strings.Index(rest, "/"); i >= 0 {
					p := opts.Prefix + rest[:i+1]
					if !seenPrefix[p] {
						seenPrefix[p] = true
						out <- minio.ObjectInfo{Key: p}
					}
					continue
				}
			}
			o := f.objects[k]
			out <- minio.ObjectInfo{
				Key:          k,
				Size:         int64(len(o.data)),
				LastModified: o.modTime,
				ETag:         o.etag,
				Metadata:     o.metadata,
			}
		}
	}()
	return out
}

func (f *fakeS3) CopyObject(_ context.Context, dst minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[src.Object]
	if !ok {
		return minio.UploadInfo{}, minio.ErrorResponse{Code: "NoSuchKey", Message: "no such key"}
	}
	cp := *o
	f.objects[dst.Object] = &cp
	return minio.UploadInfo{ETag: o.etag, Size: int64(len(o.data))}, nil
}

func (f *fakeS3) PresignedGetObject(_ context.Context, bucket, object string, _ time.Duration, _ url.Values) (*url.URL, error) {
	return &url.URL{Scheme: "https", Host: "s3.example.com", Path: "/" + bucket + "/" + object}, nil
}

func (f *fakeS3) BucketExists(_ context.Context, bucket string) (bool, error) {
	return true, nil
}

func newFakeFS(t *testing.T, opts S3Options) (*s3fs, *fakeS3) {
	t.Helper()
	opts.Bucket = "test-bucket"
	if opts.Endpoint == "" {
		opts.Endpoint = "localhost:9000"
	}
	fsys, err := newS3FS(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	fake := newFakeS3()
	fsys.cli = fake
	return fsys, fake
}

func TestS3WriteReadRoundTrip(t *testing.T) {
	fsys, fake := newFakeFS(t, S3Options{Prefix: "mc"})
	root := fsys.root

	path := filepath.Join(root, "versions", "1.0.json")
	if err := fsys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"id":"1.0"}`)
	if err := fsys.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Object landed under bucket/prefix.
	fake.mu.Lock()
	obj, ok := fake.objects["mc/versions/1.0.json"]
	fake.mu.Unlock()
	if !ok {
		t.Fatalf("expected key mc/versions/1.0.json, have %v", fake.objects)
	}
	if !bytes.Equal(obj.data, data) {
		t.Fatalf("object data = %q", obj.data)
	}
	// Checksum metadata attached.
	if got := obj.metadata.Get("X-Amz-Meta-Mva-Sha1"); got == "" {
		t.Fatal("missing sha1 metadata")
	}

	got, err := fsys.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("ReadFile = %q", got)
	}

	info, err := fsys.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len(data)) || info.IsDir() {
		t.Fatalf("stat = %v size=%d", info, info.Size())
	}
	if !fsys.Exists(path) {
		t.Fatal("Exists = false")
	}
}

func TestS3OpenSeekReadAt(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{})
	path := filepath.Join(fsys.root, "big.bin")
	data := bytes.Repeat([]byte("abcd"), 1024)
	if err := fsys.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := fsys.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 8)
	if _, err := f.ReadAt(buf, 4); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "abcdabcd" {
		t.Fatalf("ReadAt = %q", buf)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(all, data) {
		t.Fatal("seek+read mismatch")
	}
}

func TestS3MissingFile(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{})
	path := filepath.Join(fsys.root, "nope.bin")
	if _, err := fsys.Open(path); !isNotExistErr(err) {
		t.Fatalf("Open err = %v", err)
	}
	if _, err := fsys.Stat(path); !isNotExistErr(err) {
		t.Fatalf("Stat err = %v", err)
	}
	if fsys.Exists(path) {
		t.Fatal("Exists = true for missing")
	}
}

func TestS3WriteFileExclusive(t *testing.T) {
	fsys, fake := newFakeFS(t, S3Options{})
	path := filepath.Join(fsys.root, "claim")
	if err := fsys.WriteFileExclusive(path, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Second exclusive write must fail with ErrExist.
	fake.rejectExisting = true
	if err := fsys.WriteFileExclusive(path, []byte("b"), 0o644); !isExistErr(err) {
		t.Fatalf("exclusive overwrite err = %v", err)
	}
	// Data unchanged.
	got, _ := fsys.ReadFile(path)
	if string(got) != "a" {
		t.Fatalf("data = %q", got)
	}
	// Fallback: backend ignores conditional writes but reports existence.
	fake.rejectExisting = false
	fake.failPut = true
	if err := fsys.WriteFileExclusive(path, []byte("c"), 0o644); !isExistErr(err) {
		t.Fatalf("fallback exclusive err = %v", err)
	}
	fake.failPut = false
}

func TestS3ReadDirAndWalk(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{Prefix: "p"})
	root := fsys.root
	files := map[string]string{
		"versions/1.0.json":    "{}",
		"versions/1.1.json":    "{}",
		"versions/nested/deep": "x",
		"libraries/a/b/c.jar":  "jar",
		"manifest/v2.json":     "{}",
	}
	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = p
		if err := fsys.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := fsys.ReadDir(filepath.Join(root, "versions"))
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]bool{}
	present := map[string]bool{}
	for _, e := range entries {
		present[e.Name()] = true
		dirs[e.Name()] = e.IsDir()
	}
	if !present["1.0.json"] || !present["1.1.json"] || !dirs["nested"] {
		t.Fatalf("versions entries = %v dirs=%v", present, dirs)
	}
	if dirs["1.0.json"] || len(present) != 3 {
		t.Fatalf("versions entries count = %d: %v", len(present), present)
	}

	var walked []string
	err = fsys.Walk(root, func(path string, d vfs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := fsys.relOf(path)
		walked = append(walked, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range files {
		found := false
		for _, w := range walked {
			if w == rel {
				found = true
			}
		}
		if !found {
			t.Fatalf("walk missing %s; walked=%v", rel, walked)
		}
	}
}

func TestS3KeySet(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{Prefix: "base"})
	for _, rel := range []string{"a/1", "a/2", "b/3"} {
		if err := fsys.WriteFile(filepath.Join(fsys.root, filepath.FromSlash(rel)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	set, err := fsys.KeySet()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a/1", "a/2", "b/3"} {
		if _, ok := set[want]; !ok {
			t.Fatalf("keyset missing %s: %v", want, set)
		}
	}
	if len(set) != 3 {
		t.Fatalf("keyset size = %d: %v", len(set), set)
	}
}

func TestS3DigestMetadata(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{})
	path := filepath.Join(fsys.root, "f.bin")
	data := []byte("hello world")
	if err := fsys.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Compile-time proof the capability exists; call it directly.
	var _ vfs.Digester = fsys
	d := fsys
	s1 := sha1.Sum(data) //#nosec G401 -- test fixture
	s2 := sha256.Sum256(data)
	sha1hex, sha256hex, size, ok := d.Digest(path)
	if !ok {
		t.Fatal("Digest ok = false")
	}
	if sha1hex != hex.EncodeToString(s1[:]) {
		t.Fatalf("sha1 = %s", sha1hex)
	}
	if sha256hex != hex.EncodeToString(s2[:]) {
		t.Fatalf("sha256 = %s", sha256hex)
	}
	if size != int64(len(data)) {
		t.Fatalf("size = %d", size)
	}
}

func TestS3CopyRenameRemove(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{})
	root := fsys.root
	src := filepath.Join(root, "a", "f.bin")
	dst := filepath.Join(root, "b", "g.bin")
	if err := fsys.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	var _ vfs.Copier = fsys
	copier := fsys
	if err := copier.Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := fsys.ReadFile(dst); string(got) != "payload" {
		t.Fatalf("copy dst = %q", got)
	}
	// Copy keeps the source.
	if !fsys.Exists(src) {
		t.Fatal("copy removed source")
	}
	// Rename = copy + delete for remote objects.
	if err := fsys.Rename(src, filepath.Join(root, "a", "h.bin")); err != nil {
		t.Fatal(err)
	}
	if fsys.Exists(src) {
		t.Fatal("rename kept source")
	}
	if !fsys.Exists(filepath.Join(root, "a", "h.bin")) {
		t.Fatal("rename missing dst")
	}
	if err := fsys.Remove(dst); err != nil {
		t.Fatal(err)
	}
	if fsys.Exists(dst) {
		t.Fatal("remove failed")
	}
}

func TestS3RemoveAll(t *testing.T) {
	fsys, fake := newFakeFS(t, S3Options{Prefix: "p"})
	for _, rel := range []string{"x/1", "x/y/2", "z/3"} {
		if err := fsys.WriteFile(filepath.Join(fsys.root, filepath.FromSlash(rel)), []byte("d"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var _ vfs.Purger = fsys
	purger := fsys
	if err := purger.RemoveAll(filepath.Join(fsys.root, "x")); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	remaining := len(fake.objects)
	_, hasX := fake.objects["p/x/1"]
	_, hasZ := fake.objects["p/z/3"]
	fake.mu.Unlock()
	if hasX || remaining != 1 || !hasZ {
		t.Fatalf("after RemoveAll: remaining=%d hasX=%v hasZ=%v", remaining, hasX, hasZ)
	}
}

func TestS3PresignGet(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{Prefix: "p"})
	path := filepath.Join(fsys.root, "v", "f.jar")
	if err := fsys.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var _ vfs.Redirector = fsys
	redir := fsys
	u, err := redir.PresignGet(path, 60)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "p/v/f.jar") {
		t.Fatalf("presigned url = %s", u)
	}
}

func TestS3LocalScratchStaysLocal(t *testing.T) {
	fsys, fake := newFakeFS(t, S3Options{})
	// .part files must never touch the bucket.
	part := filepath.Join(fsys.root, "dl", "file.jar.part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFile(part, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	n := len(fake.objects)
	fake.mu.Unlock()
	if n != 0 {
		t.Fatalf("part file uploaded: %d objects", n)
	}
	got, err := fsys.ReadFile(part)
	if err != nil || string(got) != "partial" {
		t.Fatalf("part read = %q err=%v", got, err)
	}
	// Rename local .part -> remote final streams the upload.
	final := filepath.Join(fsys.root, "dl", "file.jar")
	if err := fsys.Rename(part, final); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	_, ok := fake.objects["dl/file.jar"]
	fake.mu.Unlock()
	if !ok {
		t.Fatal("rename did not upload final object")
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("part file still exists after rename")
	}
}

func TestS3SpoolWriteUploadsOnClose(t *testing.T) {
	fsys, fake := newFakeFS(t, S3Options{Prefix: "sp"})
	path := filepath.Join(fsys.root, "streamed", "out.bin")
	w, err := fsys.OpenFileFlags(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("streamed content")
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	o, ok := fake.objects["sp/streamed/out.bin"]
	fake.mu.Unlock()
	if !ok {
		t.Fatal("spooled write never uploaded")
	}
	if !bytes.Equal(o.data, payload) {
		t.Fatalf("uploaded = %q", o.data)
	}
	// Spool scratch cleaned up.
	matches, _ := filepath.Glob(filepath.Join(fsys.root, ".spool", "**"))
	_ = matches
	if entries, err := os.ReadDir(filepath.Join(fsys.root, ".spool")); err == nil && len(entries) > 0 {
		t.Fatalf("spool not cleaned: %v", entries)
	}
}

func TestS3CopyFromReader(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{})
	path := filepath.Join(fsys.root, "copied.bin")
	n, err := fsys.CopyFromReader(path, strings.NewReader("hello"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("n = %d", n)
	}
	got, _ := fsys.ReadFile(path)
	if string(got) != "hel" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveS3Endpoint(t *testing.T) {
	cases := []struct {
		opts    S3Options
		wantEp  string
		wantSec bool
	}{
		{S3Options{Endpoint: "s3.us-east-1.amazonaws.com"}, "s3.us-east-1.amazonaws.com", false},
		{S3Options{Endpoint: "s3.us-east-1.amazonaws.com", SecureAuto: true}, "s3.us-east-1.amazonaws.com", true},
		{S3Options{Endpoint: "localhost:9000"}, "localhost:9000", false},
		{S3Options{Endpoint: "http://minio.local:9000"}, "minio.local:9000", false},
		{S3Options{Endpoint: "https://minio.local"}, "minio.local", true},
		{S3Options{Endpoint: "minio.example.com", Secure: false}, "minio.example.com", false},
		{S3Options{Endpoint: "minio.example.com", SecureAuto: true}, "minio.example.com", true},
	}
	for _, c := range cases {
		ep, sec := resolveS3Endpoint(c.opts)
		if ep != c.wantEp || sec != c.wantSec {
			t.Fatalf("resolveS3Endpoint(%+v) = %q,%v want %q,%v", c.opts, ep, sec, c.wantEp, c.wantSec)
		}
	}
}

func TestNormalizeS3Prefix(t *testing.T) {
	for in, want := range map[string]string{
		"":      "",
		"/":     "",
		"mc":    "mc",
		"/mc/":  "mc",
		"a//b/": "a/b",
		"a/b/c": "a/b/c",
	} {
		if got := normalizeS3Prefix(in); got != want {
			t.Fatalf("normalizeS3Prefix(%q) = %q want %q", in, got, want)
		}
	}
}

func TestS3FSIsRemote(t *testing.T) {
	fsys, _ := newFakeFS(t, S3Options{})
	if !fsys.IsRemote() {
		t.Fatal("s3fs.IsRemote = false")
	}
	var _ vfs.RemoteFS = fsys
}

// TestS3ExportImportZip exercises the full zip path against an S3-backed
// layout: export walks remote objects, import writes remote objects.
func TestS3ExportImportZip(t *testing.T) {
	root := t.TempDir()
	opts := S3Options{Bucket: "b", Endpoint: "localhost:9000", Prefix: "data"}
	fsys, err := newS3FS(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	srcFake := newFakeS3()
	fsys.cli = srcFake
	src := New(root)
	src.fsys = fsys

	for rel, body := range map[string]string{
		"versions/1.0.json":   `{"id":"1.0"}`,
		"jars/client/1.0.jar": "jar-bytes",
		"libraries/a/b.jar":   "lib",
	} {
		if err := fsys.WriteFile(src.Path(rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	zipPath := filepath.Join(t.TempDir(), "out.zip")
	res, err := ExportZip(src, zipPath, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 3 {
		t.Fatalf("exported files = %d", res.Files)
	}

	// Fresh backend simulating a different host importing the archive.
	dstRoot := t.TempDir()
	dstFS, err := newS3FS(dstRoot, opts)
	if err != nil {
		t.Fatal(err)
	}
	dstFake := newFakeS3()
	dstFS.cli = dstFake
	dst := New(dstRoot)
	dst.fsys = dstFS

	imp, err := ImportZip(dst, zipPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Files != 3 {
		t.Fatalf("imported files = %d", imp.Files)
	}
	got, err := dst.ReadFile(dst.Path("versions/1.0.json"))
	if err != nil || string(got) != `{"id":"1.0"}` {
		t.Fatalf("imported content = %q err=%v", got, err)
	}
	dstFake.mu.Lock()
	_, ok := dstFake.objects["data/jars/client/1.0.jar"]
	dstFake.mu.Unlock()
	if !ok {
		t.Fatal("imported object missing expected key data/jars/client/1.0.jar")
	}
}

// TestS3FSRealServer runs s3fs against a real minio-go client talking HTTP
// to an in-memory S3 server, exercising signing, listings, conditional
// writes, metadata, and presigning end to end.
func TestS3FSRealServer(t *testing.T) {
	faker := gofakes3.New(s3mem.New(), gofakes3.WithAutoBucket(true))
	srv := httptest.NewServer(faker.Server())
	defer srv.Close()

	endpoint := strings.TrimPrefix(srv.URL, "http://")
	cli, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4("ak", "sk", ""),
		Secure:       false,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	fsys, err := newS3FS(root, S3Options{Bucket: "bucket", Prefix: "mc", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	fsys.cli = minioClient{cli}

	// Write/read/stat.
	path := filepath.Join(root, "versions", "1.0.json")
	if err := fsys.WriteFile(path, []byte(`{"id":"1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fsys.ReadFile(path)
	if err != nil || string(got) != `{"id":"1.0"}` {
		t.Fatalf("read = %q err=%v", got, err)
	}
	if info, err := fsys.Stat(path); err != nil || info.Size() != 12 {
		t.Fatalf("stat = %v err=%v", info, err)
	}

	// Exclusive create: second write must fail.
	claim := filepath.Join(root, "coordinator", "claims", "1.0.claim")
	if err := fsys.WriteFileExclusive(claim, []byte("agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFileExclusive(claim, []byte("other"), 0o644); !isExistErr(err) {
		t.Fatalf("exclusive err = %v", err)
	}

	// ReadDir sees nested structure; Walk finds all files.
	entries, err := fsys.ReadDir(filepath.Join(root, "versions"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "1.0.json" {
		t.Fatalf("readdir = %v err=%v", entries, err)
	}
	var walked int
	if err := fsys.Walk(root, func(_ string, d vfs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			walked++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if walked != 2 {
		t.Fatalf("walked files = %d", walked)
	}

	// Digest metadata round-trips through HEAD.
	var _ vfs.Digester = fsys
	s1, s2, size, ok := fsys.Digest(path)
	if !ok || size != 12 || s1 == "" || s2 == "" {
		t.Fatalf("digest = %q %q %d %v", s1, s2, size, ok)
	}

	// Rename remote->remote copies server-side.
	moved := filepath.Join(root, "versions", "1.0-renamed.json")
	if err := fsys.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if fsys.Exists(path) {
		t.Fatal("rename left source")
	}
	if got, _ := fsys.ReadFile(moved); string(got) != `{"id":"1.0"}` {
		t.Fatalf("moved content = %q", got)
	}

	// Presigned URL is produced (signature validity is minio-go's concern).
	u, err := fsys.PresignGet(moved, 60)
	if err != nil || !strings.Contains(u, "1.0-renamed.json") {
		t.Fatalf("presign = %q err=%v", u, err)
	}

	// .part stays local.
	part := filepath.Join(root, "x.jar.part")
	if err := fsys.WriteFile(part, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.StatObject(context.Background(), "bucket", "mc/x.jar.part", minio.StatObjectOptions{}); err == nil {
		t.Fatal("part file reached the bucket")
	}
}
