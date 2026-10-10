package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"minecraft-version-archive/internal/safepath"
)

func verifySHA256Bytes(data []byte, want string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !equalHex(got, want) {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, want)
	}
	return nil
}

func FileSHA256(dir *safepath.Dir, abs string) (sha256hex string, size int64, err error) {
	f, err := dir.OpenFile(abs)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func FromSHA256(checksum string, size int64) Spec {
	return Spec{SHA256: checksum, Size: size}
}
