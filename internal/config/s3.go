package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"minecraft-version-archive/internal/constants"
)

// S3Config selects an S3-compatible object storage backend for the data
// directory. When Enabled is false the archive lives on the local filesystem
// under DataDir as before.
type S3Config struct {
	Enabled      bool
	Bucket       string
	Prefix       string
	Endpoint     string // host[:port] or http(s):// URL; empty = AWS S3
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	Anonymous    bool // never sign requests (public buckets)
	PathStyle    bool // path-style URLs (bucket in path)
	Secure       bool // use HTTPS for scheme-less endpoints
	SecureAuto   bool // derive HTTPS from endpoint scheme / localhost
	Redirect     bool // serve objects via presigned URL redirects
	RedirectTTL  time.Duration
}

func loadS3(ini map[string]map[string]string) S3Config {
	sec := constants.INISectionS3
	s3 := S3Config{
		Enabled:      getBool(ini, sec, "enabled", false),
		Bucket:       get(ini, sec, "bucket", ""),
		Prefix:       get(ini, sec, "prefix", ""),
		Endpoint:     get(ini, sec, "endpoint", ""),
		Region:       get(ini, sec, "region", ""),
		AccessKey:    get(ini, sec, "access_key", ""),
		SecretKey:    get(ini, sec, "secret_key", ""),
		SessionToken: get(ini, sec, "session_token", ""),
		Anonymous:    getBool(ini, sec, "anonymous", false),
		PathStyle:    getBool(ini, sec, "path_style", false),
		Secure:       getBool(ini, sec, "use_ssl", true),
		Redirect:     getBool(ini, sec, "redirect", false),
	}
	// use_ssl only applies when the key was explicitly configured; otherwise
	// the backend auto-detects (endpoint scheme wins, localhost defaults to
	// plain HTTP).
	if rawUseSSL(ini) == "" {
		s3.SecureAuto = true
		s3.Secure = true
	}
	if v := get(ini, sec, "redirect_ttl", ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			s3.RedirectTTL = d
		}
	}
	return s3
}

// rawUseSSL reports the raw configured value so we can distinguish
// "unset" from an explicit false.
func rawUseSSL(ini map[string]map[string]string) string {
	if s, ok := ini[constants.INISectionS3]; ok {
		return strings.TrimSpace(s["use_ssl"])
	}
	return ""
}

func applyS3Env(s3 *S3Config) {
	e := constants.EnvPrefix
	if v := getenv(e + "S3_ENABLED"); v != "" {
		s3.Enabled = parseBool(v)
	}
	if v := getenv(e + "S3_BUCKET"); v != "" {
		s3.Bucket = v
	}
	if v := getenv(e + "S3_PREFIX"); v != "" {
		s3.Prefix = v
	}
	if v := getenv(e + "S3_ENDPOINT"); v != "" {
		s3.Endpoint = v
	}
	if v := getenv(e + "S3_REGION"); v != "" {
		s3.Region = v
	}
	if v := getenv(e + "S3_ACCESS_KEY"); v != "" {
		s3.AccessKey = v
	}
	if v := getenv(e + "S3_SECRET_KEY"); v != "" {
		s3.SecretKey = v
	}
	if v := getenv(e + "S3_SESSION_TOKEN"); v != "" {
		s3.SessionToken = v
	}
	if v := getenv(e + "S3_ANONYMOUS"); v != "" {
		s3.Anonymous = parseBool(v)
	}
	if v := getenv(e + "S3_PATH_STYLE"); v != "" {
		s3.PathStyle = parseBool(v)
	}
	if v := getenv(e + "S3_USE_SSL"); v != "" {
		s3.Secure = parseBool(v)
		s3.SecureAuto = false
	}
	if v := getenv(e + "S3_REDIRECT"); v != "" {
		s3.Redirect = parseBool(v)
	}
	if v := getenv(e + "S3_REDIRECT_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			s3.RedirectTTL = d
		}
	}
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func (s3 *S3Config) validate() error {
	if !s3.Enabled {
		return nil
	}
	if strings.TrimSpace(s3.Bucket) == "" {
		return fmt.Errorf("s3 backend enabled but no bucket configured (s3.bucket or MVA_S3_BUCKET)")
	}
	if s3.Anonymous && (s3.AccessKey != "" || s3.SecretKey != "") {
		return fmt.Errorf("s3.anonymous cannot be combined with access_key/secret_key")
	}
	if s3.RedirectTTL < 0 {
		return fmt.Errorf("s3.redirect_ttl must be positive")
	}
	return nil
}
