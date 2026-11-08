package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.ini")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadS3DisabledByDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.Enabled {
		t.Fatal("S3 should be disabled by default")
	}
}

func TestLoadS3Section(t *testing.T) {
	path := writeSettings(t, `[s3]
enabled = true
bucket = my-bucket
prefix = mc/data
endpoint = https://s3.example.com
region = us-east-1
access_key = AK
secret_key = SK
path_style = true
redirect = true
redirect_ttl = 30m
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s3 := cfg.S3
	if !s3.Enabled || s3.Bucket != "my-bucket" || s3.Prefix != "mc/data" {
		t.Fatalf("S3 = %+v", s3)
	}
	if s3.Endpoint != "https://s3.example.com" || s3.Region != "us-east-1" {
		t.Fatalf("endpoint/region = %q %q", s3.Endpoint, s3.Region)
	}
	if s3.AccessKey != "AK" || s3.SecretKey != "SK" {
		t.Fatalf("credentials = %q %q", s3.AccessKey, s3.SecretKey)
	}
	if !s3.PathStyle || !s3.Redirect || s3.RedirectTTL.String() != "30m0s" {
		t.Fatalf("flags = %+v", s3)
	}
}

func TestLoadS3EnvOverrides(t *testing.T) {
	path := writeSettings(t, `[s3]
enabled = true
bucket = ini-bucket
`)
	t.Setenv("MVA_S3_BUCKET", "env-bucket")
	t.Setenv("MVA_S3_ENDPOINT", "minio.local:9000")
	t.Setenv("MVA_S3_ANONYMOUS", "true")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.Bucket != "env-bucket" {
		t.Fatalf("bucket = %q", cfg.S3.Bucket)
	}
	if cfg.S3.Endpoint != "minio.local:9000" || !cfg.S3.Anonymous {
		t.Fatalf("S3 = %+v", cfg.S3)
	}
}

func TestLoadS3ExampleFileParses(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "settings.ini.example"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.Enabled {
		t.Fatal("example file should keep s3 disabled")
	}
}

func TestLoadS3Validation(t *testing.T) {
	path := writeSettings(t, `[s3]
enabled = true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected missing bucket error")
	}

	path = writeSettings(t, `[s3]
enabled = true
bucket = b
anonymous = true
access_key = AK
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected anonymous+credentials conflict error")
	}
}
