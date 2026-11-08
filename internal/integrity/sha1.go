package integrity

import (
	"crypto/sha1" //#nosec G505 -- Mojang artifact checksums use SHA-1.
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"minecraft-version-archive/internal/vfs"
)

type Spec struct {
	SHA1   string
	SHA256 string
	Size   int64
}

func (s Spec) Known() bool {
	return s.SHA1 != "" || s.SHA256 != ""
}

func VerifyBytes(data []byte, spec Spec) error {
	if !spec.Known() {
		return nil
	}
	if spec.SHA256 != "" {
		if err := verifySHA256Bytes(data, spec.SHA256); err != nil {
			return err
		}
	} else if spec.SHA1 != "" {
		sum := sha1.Sum(data) //#nosec G401 -- Mojang artifact checksums use SHA-1.
		got := hex.EncodeToString(sum[:])
		if !equalHex(got, spec.SHA1) {
			return fmt.Errorf("sha1 mismatch: got %s want %s", got, spec.SHA1)
		}
	}
	if spec.Size > 0 && int64(len(data)) != spec.Size {
		return fmt.Errorf("size mismatch: got %d want %d", len(data), spec.Size)
	}
	return nil
}

// VerifyFile checks path against spec. When the backend stores content
// checksums (vfs.Digester, e.g. object metadata) verification is a metadata
// lookup; otherwise the file is streamed through the hash.
func VerifyFile(fsys vfs.FS, abs string, spec Spec) error {
	if !spec.Known() {
		return nil
	}
	if d, ok := fsys.(vfs.Digester); ok {
		if sha1hex, sha256hex, size, ok := d.Digest(abs); ok {
			if done, err := verifyStoredDigest(abs, spec, sha1hex, sha256hex, size); done {
				return err
			}
		}
	}
	if spec.SHA256 != "" {
		got, size, err := FileSHA256(fsys, abs)
		if err != nil {
			return err
		}
		if !equalHex(got, spec.SHA256) {
			return fmt.Errorf("sha256 mismatch for %s: got %s want %s", abs, got, spec.SHA256)
		}
		if spec.Size > 0 && size != spec.Size {
			return fmt.Errorf("size mismatch for %s: got %d want %d", abs, size, spec.Size)
		}
		return nil
	}
	got, size, err := FileDigest(fsys, abs)
	if err != nil {
		return err
	}
	if !equalHex(got, spec.SHA1) {
		return fmt.Errorf("sha1 mismatch for %s: got %s want %s", abs, got, spec.SHA1)
	}
	if spec.Size > 0 && size != spec.Size {
		return fmt.Errorf("size mismatch for %s: got %d want %d", abs, size, spec.Size)
	}
	return nil
}

// verifyStoredDigest compares spec against backend-stored checksums. done is
// false when the stored metadata lacks the digest the spec requires, in
// which case the caller must fall back to streaming verification.
func verifyStoredDigest(abs string, spec Spec, sha1hex, sha256hex string, size int64) (done bool, err error) {
	if spec.SHA256 != "" {
		if sha256hex == "" {
			return false, nil
		}
		if !equalHex(sha256hex, spec.SHA256) {
			return true, fmt.Errorf("sha256 mismatch for %s: got %s want %s", abs, sha256hex, spec.SHA256)
		}
		if spec.Size > 0 && size != spec.Size {
			return true, fmt.Errorf("size mismatch for %s: got %d want %d", abs, size, spec.Size)
		}
		return true, nil
	}
	if spec.SHA1 != "" {
		if sha1hex == "" {
			return false, nil
		}
		if !equalHex(sha1hex, spec.SHA1) {
			return true, fmt.Errorf("sha1 mismatch for %s: got %s want %s", abs, sha1hex, spec.SHA1)
		}
		if spec.Size > 0 && size != spec.Size {
			return true, fmt.Errorf("size mismatch for %s: got %d want %d", abs, size, spec.Size)
		}
		return true, nil
	}
	return false, nil
}

func FileDigest(fsys vfs.FS, abs string) (sha1hex string, size int64, err error) {
	f, err := fsys.Open(abs)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha1.New() //#nosec G401 -- Mojang artifact checksums use SHA-1.
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func equalHex(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func FromArtifact(sha1 string, size int64) Spec {
	return Spec{SHA1: sha1, Size: size}
}
