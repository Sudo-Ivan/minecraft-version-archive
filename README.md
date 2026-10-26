# minecraft-version-archive

A minimal Go archive and HTTP mirror for Minecraft Java Edition client and server files.

Use it to download and self-host version manifests, jars, libraries, and assets so launchers (Prism, PolyMC, FreeSM, and others) can keep working if official Mojang endpoints change or become unavailable.

## Quick start

No config file required.

```bash
go build -o bin/mva ./cmd/mva/

./bin/mva archive          # download latest release into ./data
./bin/mva serve            # serve on http://localhost:8080
```

Or run the server immediately (serves whatever is already in `./data`):

```bash
./bin/mva
```

`mva help` prints usage. If `settings.ini` exists in the working directory it is loaded automatically. Environment variables override any setting.

## Requirements

- Go 1.27 or newer
- Network access for archiving from Mojang

## Build

```bash
go build -o bin/mva ./cmd/mva/
```

## Commands

| Command | Purpose |
|---------|---------|
| `mva` / `mva serve` | Start the HTTP mirror |
| `mva archive` | Download versions from Mojang into `data_dir` |
| `mva archive-fabric` | Download Fabric loader metadata and libraries for archived MC versions |
| `mva export` | Zip `data_dir` for backup or transfer |
| `mva import` | Restore `data_dir` from a zip |
| `mva version` | Print tool version |
| `mva help` | Show usage |

### Archive

```bash
./bin/mva archive                         # latest release
./bin/mva archive 1.20.1                  # specific version
./bin/mva archive --version 1.5.0         # alias: resolves to 1.5
./bin/mva archive --all                   # all releases (~101)
./bin/mva archive --all --include-snapshots --include-prereleases
./bin/mva archive --all --include-all-types
./bin/mva archive --versions-file versions.txt
```

Version aliases: trailing `.0` segments are stripped automatically (`1.5.0` becomes `1.5`). Unknown IDs show nearby suggestions.

`--all` archives every **release** by default. Add include flags for other manifest types:

| Flag / setting | Includes |
|----------------|----------|
| `--include-snapshots` / `include_snapshots` | Weekly and numbered snapshots (`26w14a`, `26.1-snapshot-11`) |
| `--include-prereleases` / `include_prereleases` | Pre-releases (`26.1-pre-1`) |
| `--include-rc` / `include_rc` | Release candidates (`26.1-rc-2`) |
| `--include-alpha` / `include_alpha` | `old_alpha` versions |
| `--include-beta` / `include_beta` | `old_beta` versions |
| `--include-all-types` | All of the above |

### Fabric

Archive Fabric loader metadata and Maven artifacts after vanilla versions are in `data/`:

```bash
./bin/mva archive 1.21.1                    # vanilla first
./bin/mva archive-fabric                  # fabric for every archived MC version
./bin/mva archive-fabric 1.21.1           # one MC version
./bin/mva archive-fabric --loader 0.16.9 1.20.1
./bin/mva archive-fabric --all-loaders 1.21.1
```

By default, `archive-fabric` picks the latest **stable** Fabric loader per Minecraft version. `--all-loaders` archives every compatible loader (larger download).

Stored under:

```
data/fabric/meta/v2/...       # meta.fabricmc.net responses
data/fabric/maven/...         # maven.fabricmc.net artifacts
```

### Serve

```bash
./bin/mva serve
./bin/mva serve --port 8077
./bin/mva serve --host 127.0.0.1 --port 8077 --trust-proxy
./bin/mva serve --public-url https://minecraft.example.com
./bin/mva serve --proxy-on-miss
./bin/mva serve --proxy-on-miss --no-proxy-cache
```

Run behind nginx or Caddy with TLS. Set `public_url` to your external URL and enable `trust_proxy` when the proxy sets `X-Forwarded-Proto` and `X-Forwarded-Host`.

### Missing files and upstream proxy

By default, requests for files not on disk return **HTTP 404**. The local manifest only lists archived versions, so launchers normally never request versions you have not archived.

Enable on-demand fetching from Mojang:

```bash
./bin/mva serve --proxy-on-miss
```

Or in settings:

```ini
[server]
proxy_on_miss = true
proxy_cache = true
```

| Setting | Default | Effect |
|---------|---------|--------|
| `proxy_on_miss` | `false` | Fetch missing manifest, version JSON, jars, libraries, assets, Fabric meta, and Fabric Maven from upstream |
| `proxy_cache` | `true` | Save proxied files into `data_dir` for later requests |
| `--no-proxy-cache` | off | Passthrough only: serve from Mojang without writing to disk |

Environment: `MVA_PROXY_ON_MISS=true`, `MVA_PROXY_CACHE=false` for passthrough.

When `proxy_cache` is enabled, the first request for a missing file downloads from Mojang, serves it to the client, and stores it locally. Later requests are served from disk like archived content.

### Export and import

```bash
./bin/mva export                              # full data folder
./bin/mva export backup.zip
./bin/mva export -output partial.zip -version 1.20.1 -version 1.19.4
./bin/mva import backup.zip                   # merge into data_dir
./bin/mva import -replace backup.zip          # wipe data_dir first
```

Partial exports include the filtered manifest, version JSON, jars, libraries, asset indexes, and asset objects for the selected versions. Coordinator claims are never exported.

Uses `zip.Store` (no recompression) for speed on already-compressed game files.

## Configuration

Settings are optional. With no `settings.ini` and no environment variables, these defaults apply:

| Setting | Default |
|---------|---------|
| Data directory | `./data` |
| Listen address | `:8080` |
| Public URL | `http://localhost:8080` (derived from listen) |
| Archive target | Latest Mojang release |
| Cache | Enabled, 512 MB, 512 entries, 256 shards |

To customize, copy the example settings file:

```bash
cp settings.ini.example settings.ini
```

Settings load from `settings.ini` when present. Use `-settings path` to pick a different file, or `MVA_SETTINGS` in the environment. All other `MVA_*` variables override INI values.

### settings.ini sections

| Section | Purpose |
|---------|---------|
| `general` | Data directory, listen address, public URL |
| `archive` | Version selection, include filters, workers, coordinator, retry policy |
| `cache` | In-memory cache for hot downloads |
| `server` | Inventory cache TTL, reverse-proxy trust |
| `stats` | Request and throughput metrics endpoint |
| `p2p` | Peer instances for federated fetching |

### Version selection

For `mva archive`, one of the following applies. If none are set, the **latest release** is archived.

- Default (no config): latest release
- CLI: `mva archive 1.20.1` or `--version` or `--all`
- `archive_all = true` in `[archive]` (releases only unless include flags are set)
- `versions = 1.20.1,1.19.4`
- `versions_file = versions.txt`
- `MVA_ARCHIVE_ALL=true` or `MVA_VERSIONS` or `MVA_VERSIONS_FILE`

### Client and server modes

| Setting | Effect |
|---------|--------|
| `client_only = true` | Archives client jar, libraries, and assets only |
| `server_only = true` | Archives server jars only |

### Cache size

Set one of:

```ini
max_mb = 512
max_gb = 1
```

Environment: `MVA_CACHE_MB` or `MVA_CACHE_GB`.

For high traffic, increase entries and shards:

```ini
[cache]
max_entries = 2048
max_mb = 1024
shards = 256
```

`shards` splits the cache into independent LRU segments to reduce lock contention under thousands of concurrent downloads.

### Server tuning

```ini
[server]
inventory_ttl_sec = 30
trust_proxy = true
```

`inventory_ttl_sec` caches `/p2p/inventory` and `/java` index responses so those listings do not walk the full data directory on every request.

`trust_proxy` uses `X-Forwarded-Proto` and `X-Forwarded-Host` when rewriting manifest and version JSON URLs behind a reverse proxy.

### Common environment variables

| Variable | Description |
|----------|-------------|
| `MVA_DATA_DIR` | Storage root (default `./data`) |
| `MVA_SETTINGS` | Path to settings.ini |
| `MVA_LISTEN` | Bind address (default `:8080`) |
| `MVA_PUBLIC_URL` | Base URL written into mirrored manifests |
| `MVA_WORKERS` | Parallel archive workers (default 8) |
| `MVA_AGENT_ID` | Coordinator agent name for multi-instance archive |
| `MVA_COORDINATOR_STALE` | Seconds before reclaiming stale version claims |
| `MVA_INCLUDE_SNAPSHOTS` | Include snapshots when `archive_all` is set |
| `MVA_INCLUDE_PRERELEASES` | Include pre-releases when `archive_all` is set |
| `MVA_INCLUDE_RC` | Include release candidates when `archive_all` is set |
| `MVA_INCLUDE_ALPHA` | Include old_alpha versions when `archive_all` is set |
| `MVA_INCLUDE_BETA` | Include old_beta versions when `archive_all` is set |
| `MVA_CACHE_SHARDS` | Cache shard count (default 256) |
| `MVA_INVENTORY_TTL` | Inventory cache TTL in seconds |
| `MVA_TRUST_PROXY` | Trust reverse-proxy forwarded headers |
| `MVA_PROXY_ON_MISS` | Fetch missing files from Mojang on request |
| `MVA_PROXY_CACHE` | Save proxied files to `data_dir` (`false` = passthrough) |
| `MVA_STATS_ENABLED` | Enable `GET /stats` |
| `MVA_P2P_ENABLED` | Query peers before Mojang for missing files |
| `MVA_PEERS` | Comma-separated peer base URLs |

## Parallel archive and coordination

`workers` controls how many versions download at the same time (default 8). Within each version, libraries and assets also download concurrently.

Multiple `mva archive` processes can share the same `data_dir`. A file-based coordinator assigns each version to one agent at a time using claims under `data/coordinator/`. Completed versions are marked in `data/coordinator/done/` so agents skip them.

Set a unique `agent_id` or `MVA_AGENT_ID` per machine when running several archivers. Stale claims are reclaimed after `coordinator_stale_sec` (default 3600).

Progress logs every 2 seconds with human-readable output:

```
Progress 186/4860 files (3.8%)  |  46.7 MB  |  23.3 MB/s  |  26.1.2
```

## Integrity verification

Downloads are verified against Mojang SHA1 and size metadata when those fields are present. Existing files are checked before they are skipped; corrupt or truncated files are removed and re-fetched. Asset objects served from `/assets/objects/{aa}/{hash}` are verified against the hash in the URL path before delivery.

## Hash deduplication

A persistent SHA1 index at `data/inventory/sha1_index.json` records every verified object. Before downloading, the archiver looks up the expected hash and reuses an existing file via hard link (or copy when linking is not possible). Asset objects under `data/assets/objects/` are indexed automatically on startup. Identical jars, libraries, and indexes across versions therefore share disk space instead of being stored twice.

## HTTP endpoints

| Path | Description |
|------|-------------|
| `/` | Embedded landing page for browsers (`Accept: text/html`); JSON index with public URL and endpoint list otherwise (`?json` forces JSON) |
| `/docs` | API reference page |
| `/web/{file}` | Site assets (stylesheets, fonts) |
| `/mc/game/version_manifest_v2.json` | Mirrored manifest with local URLs |
| `/versions/{id}.json` | Version metadata |
| `/java` | Bulk Java runtime index (`major_version`, `component` per version) |
| `/java/{id}` | Java runtime for one version (alias-aware, e.g. `1.5.0` -> `1.5`) |
| `/meta/v1/{path}` | PolyMC-compatible Fabric meta (`index.json`, `net.fabricmc.fabric-loader/...`, `net.fabricmc.intermediary/...`) |
| `/fabric/v2/{path}` | Fabric Meta API mirror (`meta.fabricmc.net/v2/...`) |
| `/maven/fabricmc/{path}` | Fabric Maven mirror (`maven.fabricmc.net/...`) |
| `/jars/client/{id}.jar` | Client jar |
| `/jars/server/{id}.jar` | Server jar |
| `/libraries/{path...}` | Maven-style libraries |
| `/assets/indexes/{id}.json` | Asset index |
| `/assets/objects/{aa}/{hash}` | Asset object |
| `/p2p/inventory` | Peer inventory for federation |
| `/stats` | Metrics (when enabled) |
| `/health` | Health check |

Range requests and resumable downloads are supported. Popular files can be cached in memory to reduce disk reads.

Manifest and version JSON URLs are rewritten at serve time using `public_url` or reverse-proxy headers when `trust_proxy` is enabled. Fabric meta responses rewrite `maven.fabricmc.net` library URLs to `{public_url}/maven/fabricmc/`.

### Fabric launcher setup

Point a Fabric-aware tool at your mirror:

| Upstream | Mirror |
|----------|--------|
| `https://meta.fabricmc.net/v2/...` | `https://your-host/fabric/v2/...` |
| `https://maven.fabricmc.net/...` | `https://your-host/maven/fabricmc/...` |

PolyMC offline Fabric (set mirror URL in Settings -> APIs -> Services):

| PolyMC meta path | Mirror |
|------------------|--------|
| `https://meta.polymc.org/v1/net.fabricmc.fabric-loader/...` | `https://your-host/meta/v1/net.fabricmc.fabric-loader/...` |
| `https://meta.polymc.org/v1/net.fabricmc.intermediary/...` | `https://your-host/meta/v1/net.fabricmc.intermediary/...` |

Append `?minecraft={mcVersion}` to loader index/version requests to scope results to one game version.

Java version matching for launchers:

| Endpoint | Response |
|----------|----------|
| `GET /java/{mcVersion}` | `{"id":"1.21.1","major_version":21,"component":"java-runtime-delta"}` |
| `GET /java` | Index of all archived versions with Java requirements |

Vanilla files still come from the Mojang paths on the same host (`/mc/game/version_manifest_v2.json`, `/versions/`, `/libraries/`, etc.).

## High concurrency

The server is designed for thousands of simultaneous launcher downloads.

- **Sharded in-memory cache** spreads hot file lookups across 256 independent locks by default
- **Coalesced fills** ensure one goroutine loads a file into cache, builds the peer inventory, or fetches an upstream URL while concurrent requests share the result
- **Sendfile serving** streams large jars and libraries directly from disk without buffering in memory
- **Streaming proxy** downloads proxy-on-miss jars and libraries straight to disk (SHA1-verified) instead of buffering them in memory, or relays passthrough responses without storing them
- **Rendered-JSON cache** rewrites the manifest and version JSON only when the file on disk changes
- **Integrity cache** skips re-hashing verified asset objects and Temurin binaries until size or mtime changes
- **Inventory caching** avoids full directory walks on every peer listing and `/java` index request
- **Java lookup cache** stores per-version `/java/{id}` JSON after first read
- **Conditional requests** serve `ETag`, `Cache-Control: immutable`, and 304 responses for content-addressed files
- **Gzip JSON** compresses manifest, version, meta, and index responses for clients that send `Accept-Encoding: gzip`, with the compressed body cached next to the rendered one
- **Prism meta components** generate `net.minecraft`, `org.lwjgl`, `org.lwjgl3`, and `net.minecraft.java` component indexes and version files from archived data, so Prism-style launchers can install vanilla and Java from the mirror
- **Zero-allocation hot paths** for cache hits, range parsing, and stats recording

Recommended production settings:

```ini
[cache]
max_entries = 2048
max_mb = 1024
shards = 256

[server]
inventory_ttl_sec = 60
trust_proxy = true
```

Also raise OS limits before heavy load:

```bash
ulimit -n 65535
```

Run behind a reverse proxy (nginx, Caddy) if you need TLS termination or additional connection buffering. Bind to `127.0.0.1` and terminate TLS at the proxy for public deployment.

## Launcher setup

Point your launcher custom meta server at:

```
https://your-host/mc/game/version_manifest_v2.json
```

Set `public_url` in settings to the same base URL so rewritten version JSON points back to your instance.

## P2P federation

When P2P is enabled, each instance exposes `/p2p/inventory`. During archiving, peers are checked for versions and files before falling back to Mojang.

```ini
[p2p]
enabled = true
peers = http://peer1:8080,http://peer2:8080
```

## Stats

Enable with `stats.enabled = true` or `MVA_STATS_ENABLED=true`. `GET /stats` returns JSON including:

- Total requests and bytes served
- Cache hits and misses
- Archive download counts
- Current cache usage and stored version count
- Serve throughput (10 second window and session average)

## Tests

```bash
go test ./...
go test -race -timeout 2m ./...
go test -short ./...          # skips live Mojang network tests
go test -bench=. -benchmem ./internal/...
```

Use `-timeout 2m` with the full suite so a stuck coordinator or network test fails instead of hanging indefinitely. Live Mojang integration tests verify manifest, version JSON, and library downloads against real endpoints. They run by default and fail if Mojang is unreachable or responses are invalid.

## License

Copyright 2026 Ivan

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for the full text.
