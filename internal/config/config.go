package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/safepath"
)

type Config struct {
	DataDir             string
	Listen              string
	PublicURL           string
	ArchiveAll          bool
	VersionsFile        string
	Versions            []string
	IncludeSnapshots    bool
	IncludePreReleases  bool
	IncludeRC           bool
	IncludeAlpha        bool
	IncludeBeta         bool
	ClientOnly          bool
	ServerOnly          bool
	Workers             int
	CoordinatorStaleSec int
	AgentID             string
	RetryMax            int
	RetryBaseMS         int
	CacheEnabled        bool
	CacheEntries        int
	CacheBytes          int64
	CacheShards         int
	InventoryTTL        int
	TrustProxy          bool
	ProxyOnMiss         bool
	ProxyCache          bool
	StatsEnabled        bool
	StatsStdout         bool
	BlockScanners       bool
	P2PEnabled          bool
	Peers               []string
	P2PTimeout          int
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = ResolveSettingsPath()
	}

	var ini map[string]map[string]string
	if path != "" {
		var err error
		ini, err = parseINI(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if ini == nil {
		ini = make(map[string]map[string]string)
	}

	cfg := &Config{
		DataDir:             get(ini, constants.INISectionGeneral, "data_dir", constants.DefaultDataDir),
		Listen:              get(ini, constants.INISectionGeneral, "listen", constants.DefaultListen),
		PublicURL:           strings.TrimRight(get(ini, constants.INISectionGeneral, "public_url", constants.DefaultPublic), "/"),
		ArchiveAll:          getBool(ini, constants.INISectionArchive, "archive_all", false),
		VersionsFile:        get(ini, constants.INISectionArchive, "versions_file", ""),
		IncludeSnapshots:    getBool(ini, constants.INISectionArchive, "include_snapshots", false),
		IncludePreReleases:  getBool(ini, constants.INISectionArchive, "include_prereleases", false),
		IncludeRC:           getBool(ini, constants.INISectionArchive, "include_rc", false),
		IncludeAlpha:        getBool(ini, constants.INISectionArchive, "include_alpha", false),
		IncludeBeta:         getBool(ini, constants.INISectionArchive, "include_beta", false),
		ClientOnly:          getBool(ini, constants.INISectionArchive, "client_only", false),
		ServerOnly:          getBool(ini, constants.INISectionArchive, "server_only", false),
		Workers:             getInt(ini, constants.INISectionArchive, "workers", constants.DefaultWorkers),
		CoordinatorStaleSec: getInt(ini, constants.INISectionArchive, "coordinator_stale_sec", constants.DefaultCoordinatorStale),
		AgentID:             get(ini, constants.INISectionArchive, "agent_id", ""),
		RetryMax:            getInt(ini, constants.INISectionArchive, "retry_max", constants.DefaultRetryMax),
		RetryBaseMS:         getInt(ini, constants.INISectionArchive, "retry_base_ms", constants.DefaultRetryBaseMS),
		CacheEnabled:        getBool(ini, constants.INISectionCache, "enabled", true),
		CacheEntries:        getInt(ini, constants.INISectionCache, "max_entries", constants.DefaultCacheEntries),
		CacheShards:         getInt(ini, constants.INISectionCache, "shards", constants.DefaultCacheShards),
		InventoryTTL:        getInt(ini, constants.INISectionServer, "inventory_ttl_sec", constants.DefaultInventoryTTL),
		TrustProxy:          getBool(ini, constants.INISectionServer, "trust_proxy", false),
		ProxyOnMiss:         getBool(ini, constants.INISectionServer, "proxy_on_miss", false),
		ProxyCache:          getBool(ini, constants.INISectionServer, "proxy_cache", true),
		StatsEnabled:        getBool(ini, constants.INISectionStats, "enabled", false),
		StatsStdout:         getBool(ini, constants.INISectionStats, "stdout", true),
		BlockScanners:       getBool(ini, constants.INISectionServer, "block_scanners", true),
		P2PEnabled:          getBool(ini, constants.INISectionP2P, "enabled", false),
		P2PTimeout:          getInt(ini, constants.INISectionP2P, "timeout_sec", constants.DefaultP2PTimeout),
	}

	cacheBytes, err := resolveCacheBytes(ini)
	if err != nil {
		return nil, err
	}
	cfg.CacheBytes = cacheBytes

	if v := get(ini, constants.INISectionArchive, "versions", ""); v != "" {
		cfg.Versions = splitList(v)
	}
	if p := get(ini, constants.INISectionP2P, "peers", ""); p != "" {
		cfg.Peers = splitList(p)
	}

	applyEnv(cfg)
	applyDerivedDefaults(cfg)
	if cfg.AgentID == "" {
		cfg.AgentID = coordinatorAgentID()
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func ResolveSettingsPath() string {
	if v := os.Getenv(constants.EnvPrefix + "SETTINGS"); v != "" {
		return v
	}
	if _, err := os.Stat(constants.DefaultSettingsFile); err == nil {
		return constants.DefaultSettingsFile
	}
	return ""
}

func Default() *Config {
	cfg, err := Load("")
	if err != nil {
		panic(err)
	}
	return cfg
}

func (c *Config) HasExplicitArchiveSelection() bool {
	return c.ArchiveAll || c.VersionsFile != "" || len(c.Versions) > 0
}

func applyDerivedDefaults(c *Config) {
	if c.PublicURL == "" || c.PublicURL == constants.DefaultPublic {
		c.PublicURL = derivePublicURL(c.Listen)
	}
}

func (c *Config) ApplyServeOverrides(host string, port int, positional string, publicURL string) error {
	oldListen := c.Listen
	oldDerived := derivePublicURL(oldListen)
	wasAutoPublic := c.PublicURL == "" || c.PublicURL == constants.DefaultPublic || c.PublicURL == oldDerived

	baseHost, basePort, err := splitListen(c.Listen)
	if err != nil {
		return err
	}

	if positional != "" {
		if strings.Contains(positional, ":") {
			baseHost, basePort, err = splitListen(positional)
			if err != nil {
				return fmt.Errorf("listen address: %w", err)
			}
		} else {
			basePort, err = strconv.Atoi(positional)
			if err != nil || basePort < 1 || basePort > 65535 {
				return fmt.Errorf("invalid port %q", positional)
			}
		}
	}

	if host != "" {
		baseHost = host
	}
	if port != 0 {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid port %d", port)
		}
		basePort = port
	}
	if basePort == 0 {
		basePort = 8080
	}

	c.Listen = formatListen(baseHost, basePort)
	if publicURL != "" {
		c.PublicURL = strings.TrimRight(publicURL, "/")
	} else if wasAutoPublic {
		c.PublicURL = derivePublicURL(c.Listen)
	}
	return nil
}

func splitListen(addr string) (host string, port int, err error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	port, err = strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port in %q: %w", addr, err)
	}
	return host, port, nil
}

func formatListen(host string, port int) string {
	if host == "" {
		return fmt.Sprintf(":%d", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func derivePublicURL(listen string) string {
	if listen == "" {
		return constants.DefaultPublic
	}
	host := listen
	switch {
	case strings.HasPrefix(host, "0.0.0.0:"):
		host = "localhost:" + host[len("0.0.0.0:"):]
	case strings.HasPrefix(host, "[::]:"):
		host = "localhost:" + host[len("[::]:"):]
	case strings.HasPrefix(host, ":"):
		host = "localhost" + host
	}
	if !strings.Contains(host, "://") {
		return "http://" + host
	}
	return strings.TrimRight(host, "/")
}

func resolveCacheBytes(ini map[string]map[string]string) (int64, error) {
	if gb := get(ini, constants.INISectionCache, "max_gb", ""); gb != "" {
		n, err := parseSizeValue(gb)
		if err != nil {
			return 0, fmt.Errorf("cache max_gb: %w", err)
		}
		return n * 1024 * 1024 * 1024, nil
	}
	if mb := get(ini, constants.INISectionCache, "max_mb", ""); mb != "" {
		n, err := parseSizeValue(mb)
		if err != nil {
			return 0, fmt.Errorf("cache max_mb: %w", err)
		}
		return n * 1024 * 1024, nil
	}
	if raw := get(ini, constants.INISectionCache, "max_bytes", ""); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cache max_bytes: %w", err)
		}
		return n, nil
	}
	return constants.DefaultCacheMB * 1024 * 1024, nil
}

func parseSizeValue(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, "gb")
	s = strings.TrimSuffix(s, "mb")
	s = strings.TrimSuffix(s, "g")
	s = strings.TrimSuffix(s, "m")
	s = strings.TrimSpace(s)
	return strconv.ParseInt(s, 10, 64)
}

func (c *Config) validate() error {
	if c.ClientOnly && c.ServerOnly {
		return fmt.Errorf("client_only and server_only cannot both be true")
	}
	if c.Workers < 1 {
		c.Workers = 1
	}
	if c.RetryMax < 1 {
		c.RetryMax = 1
	}
	if c.CacheEntries < 1 {
		c.CacheEntries = 1
	}
	if c.CacheShards < 1 {
		c.CacheShards = 1
	}
	if c.InventoryTTL < 1 {
		c.InventoryTTL = 1
	}
	if c.CoordinatorStaleSec < 1 {
		c.CoordinatorStaleSec = 1
	}
	if c.CacheBytes < 1 {
		return fmt.Errorf("cache size must be positive")
	}
	c.DataDir = filepath.Clean(c.DataDir)
	return nil
}

func (c *Config) ResolveVersionIDs() ([]string, error) {
	if c.ArchiveAll {
		return nil, nil
	}
	if c.VersionsFile != "" {
		ids, err := readLines(c.VersionsFile)
		if err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			return ids, nil
		}
	}
	if len(c.Versions) > 0 {
		return c.Versions, nil
	}
	return nil, fmt.Errorf("no versions configured: set archive_all, versions_file, versions, or MVA_ARCHIVE_ALL / MVA_VERSIONS")
}

func applyEnv(c *Config) {
	if v := os.Getenv(constants.EnvPrefix + "DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv(constants.EnvPrefix + "LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv(constants.EnvPrefix + "PUBLIC_URL"); v != "" {
		c.PublicURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv(constants.EnvPrefix + "ARCHIVE_ALL"); v != "" {
		c.ArchiveAll = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "INCLUDE_SNAPSHOTS"); v != "" {
		c.IncludeSnapshots = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "INCLUDE_PRERELEASES"); v != "" {
		c.IncludePreReleases = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "INCLUDE_RC"); v != "" {
		c.IncludeRC = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "INCLUDE_ALPHA"); v != "" {
		c.IncludeAlpha = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "INCLUDE_BETA"); v != "" {
		c.IncludeBeta = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "VERSIONS_FILE"); v != "" {
		c.VersionsFile = v
	}
	if v := os.Getenv(constants.EnvPrefix + "VERSIONS"); v != "" {
		c.Versions = splitList(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "CLIENT_ONLY"); v != "" {
		c.ClientOnly = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "SERVER_ONLY"); v != "" {
		c.ServerOnly = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Workers = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "AGENT_ID"); v != "" {
		c.AgentID = v
	}
	if v := os.Getenv(constants.EnvPrefix + "COORDINATOR_STALE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.CoordinatorStaleSec = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "RETRY_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryMax = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "RETRY_BASE_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryBaseMS = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "CACHE_ENABLED"); v != "" {
		c.CacheEnabled = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "CACHE_ENTRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.CacheEntries = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "CACHE_SHARDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.CacheShards = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "INVENTORY_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.InventoryTTL = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "TRUST_PROXY"); v != "" {
		c.TrustProxy = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "PROXY_ON_MISS"); v != "" {
		c.ProxyOnMiss = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "PROXY_CACHE"); v != "" {
		c.ProxyCache = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "CACHE_GB"); v != "" {
		if n, err := parseSizeValue(v); err == nil {
			c.CacheBytes = n * 1024 * 1024 * 1024
		}
	} else if v := os.Getenv(constants.EnvPrefix + "CACHE_MB"); v != "" {
		if n, err := parseSizeValue(v); err == nil {
			c.CacheBytes = n * 1024 * 1024
		}
	} else if v := os.Getenv(constants.EnvPrefix + "CACHE_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.CacheBytes = n
		}
	}
	if v := os.Getenv(constants.EnvPrefix + "STATS_ENABLED"); v != "" {
		c.StatsEnabled = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "STATS_STDOUT"); v != "" {
		c.StatsStdout = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "BLOCK_SCANNERS"); v != "" {
		c.BlockScanners = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "P2P_ENABLED"); v != "" {
		c.P2PEnabled = parseBool(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "PEERS"); v != "" {
		c.Peers = splitList(v)
	}
	if v := os.Getenv(constants.EnvPrefix + "P2P_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.P2PTimeout = n
		}
	}
}

func parseINI(path string) (map[string]map[string]string, error) {
	f, err := safepath.OpenFileAt(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sections := make(map[string]map[string]string)
	section := "default"
	sections[section] = make(map[string]string)

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			if sections[section] == nil {
				sections[section] = make(map[string]string)
			}
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(val)
		if sections[section] == nil {
			sections[section] = make(map[string]string)
		}
		sections[section][key] = val
	}
	return sections, sc.Err()
}

func get(ini map[string]map[string]string, section, key, def string) string {
	if s, ok := ini[section]; ok {
		if v, ok := s[key]; ok && v != "" {
			return v
		}
	}
	return def
}

func getBool(ini map[string]map[string]string, section, key string, def bool) bool {
	v := get(ini, section, key, "")
	if v == "" {
		return def
	}
	return parseBool(v)
}

func getInt(ini map[string]map[string]string, section, key string, def int) int {
	v := get(ini, section, key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitList(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func readLines(path string) ([]string, error) {
	f, err := safepath.OpenFileAt(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}
