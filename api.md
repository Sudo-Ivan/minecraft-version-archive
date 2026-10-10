# minecraft-version-archive HTTP API

Reference for clients (launchers, mirrors, automation agents) talking to an `mva serve` instance.

The mirror exposes Mojang-compatible paths on a single origin. Point a launcher at `{BASE}` instead of Mojang/Fabric URLs. Archived content is served from disk; missing files return **404** unless `proxy_on_miss` is enabled on the server.

## Base URL

All paths below are relative to `{BASE}`:

- Dev default: `http://localhost:8080`
- Production: whatever `public_url` / reverse proxy uses (e.g. `https://minecraft.example.com`)

Discover at runtime:

```http
GET /
```

```json
{
  "name": "minecraft-version-archive",
  "public_url": "https://minecraft.example.com",
  "endpoints": {
    "health": "/health",
    "manifest": "/mc/game/version_manifest_v2.json",
    "versions": "/versions/{file}",
    "java": "/java",
    "java_version": "/java/{id}",
    "java_temurin": "/java/temurin",
    "temurin": "/java/temurin/{major}",
    "jars": "/jars/{kind}/{file}",
    "libraries": "/libraries/{path}",
    "assets": "/assets/indexes/{file}",
    "objects": "/assets/objects/{prefix}/{hash}",
    "fabric_meta": "/fabric/v2/{path}",
    "fabric_maven": "/maven/fabricmc/{path}",
    "polymc_meta": "/meta/v1/{path}"
  }
}
```

Use `public_url` from this response (or your configured mirror URL) when rewriting links locally.

## Protocol rules

| Rule | Detail |
|------|--------|
| Methods | **GET** and **HEAD** only for game content. Other methods return **connection close** (no HTTP body) when `block_scanners` is on (default). Exception: `/p2p/` may use other methods. |
| Range | `Accept-Ranges: bytes` on file responses. Send `Range: bytes=start-end` for partial content (**206**). |
| Paths | `..`, backslashes, and unknown paths → **404**. Scanner probe paths → **TCP close** (not 404). |
| Content | Version/manifest JSON: `application/json`. Jars/libs/assets: binary streams. |
| Compression | JSON responses support `Accept-Encoding: gzip` (with `Vary` and `Content-Encoding` set). Range requests are always served uncompressed. |
| Manifest scope | `version_manifest_v2.json` lists **only versions present on this mirror**, not all Mojang releases. |

## Client implementation: vanilla Minecraft

Typical launcher flow using standard Mojang semantics:

```
1. GET {BASE}/mc/game/version_manifest_v2.json
2. Pick version id from manifest.versions[]
3. GET {BASE}/versions/{id}.json          (URL also in manifest entry)
4. From version JSON:
   - downloads.client.url  → GET client jar
   - downloads.server.url  → GET server jar (optional)
   - libraries[].downloads → GET each library artifact URL
   - assetIndex.url        → GET asset index JSON
5. For each object in asset index:
   GET {BASE}/assets/objects/{first2}/{sha1}
```

### Manifest

```http
GET /mc/game/version_manifest_v2.json
```

Same schema as Mojang `version_manifest_v2.json`. Each `versions[].url` is rewritten to:

```
{BASE}/versions/{id}.json
```

### Version metadata

```http
GET /versions/{id}.json
```

Mojang version JSON with URLs rewritten to this host:

| Field | Rewritten URL pattern |
|-------|----------------------|
| `downloads.client.url` | `{BASE}/jars/client/{id}.jar` |
| `downloads.server.url` | `{BASE}/jars/server/{id}.jar` |
| `libraries[].downloads.*.url` | `{BASE}/libraries/{maven/path}` |
| `assetIndex.url` | `{BASE}/assets/indexes/{assetIndex.id}.json` |

Original Mojang `sha1` and `size` fields are preserved. Verify downloads against them.

### Jars

```http
GET /jars/client/{id}.jar
GET /jars/server/{id}.jar
```

`{kind}` is `client` or `server`. `{id}` is the Minecraft version id (e.g. `1.21.1`).

### Libraries

```http
GET /libraries/{path...}
```

Maven layout path, e.g.:

```
/libraries/com/mojang/authlib/6.0.57/authlib-6.0.57.jar
```

Upstream equivalent: `https://libraries.minecraft.net/{path}`

### Assets

```http
GET /assets/indexes/{id}.json
GET /assets/objects/{prefix}/{hash}
```

- `{prefix}` = first two hex chars of SHA-1
- `{hash}` = full SHA-1 (40 hex chars)

Asset objects are **verified server-side** against `{hash}` before serve. If corrupt → **404** (or upstream fetch when `proxy_on_miss` is on).

### Version ID aliases

`1.5.0` resolves to `1.5` (trailing `.0` segments stripped). Applies to `/versions/`, `/java/`, and jar paths.

## Client implementation: Java runtime

Two layers: **which Java** Mojang expects, and **where to download** Temurin.

### Mojang Java requirement (per MC version)

```http
GET /java/{mcVersion}
```

```json
{
  "id": "1.21.1",
  "major_version": 21,
  "component": "java-runtime-delta",
  "temurin": "/java/temurin/21"
}
```

| Field | Meaning |
|-------|---------|
| `major_version` | Required Java major (8, 17, 21, …) |
| `component` | Mojang runtime component name (informational for Mojang-bundled runtimes) |
| `temurin` | Present when Eclipse Temurin for that major is archived on this mirror |

Bulk index:

```http
GET /java
```

```json
{
  "versions": [
    {"id":"1.21.1","major_version":21,"component":"java-runtime-delta","temurin":"/java/temurin/21"},
    {"id":"1.20.1","major_version":17,"component":"java-runtime-gamma"}
  ]
}
```

### Eclipse Temurin downloads (optional mirror feature)

Requires operator to run `mva archive-java` on the server. Not all platform/major pairs exist upstream (e.g. Java 8 has no `mac-aarch64`).

**List archived majors:**

```http
GET /java/temurin
```

```json
{"majors": [8, 17, 21]}
```

**Manifest for one major:**

```http
GET /java/temurin/{major}
```

```json
{
  "vendor": "Eclipse Temurin",
  "major": 21,
  "release_name": "jdk-21.0.11+10",
  "release_time": "2026-04-23T06:32:33Z",
  "version": {
    "name": "21.0.11+10-LTS",
    "major": 21,
    "minor": 0,
    "security": 11,
    "build": 10
  },
  "platforms": [
    {
      "runtimeOS": "linux-x64",
      "os": "linux",
      "arch": "x64",
      "file": "OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz",
      "url": "{BASE}/java/temurin/21/linux-x64/OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz",
      "packageType": "tar.gz",
      "downloadType": "Archive",
      "size": 207513939,
      "checksum": {"type": "sha-256", "hash": "..."}
    }
  ]
}
```

**Download archive:**

```http
GET /java/temurin/{major}/{platform}/{file}
```

| `{platform}` | OS / arch | `runtimeOS` (Prism-style) |
|--------------|-----------|---------------------------|
| `linux-x64` | Linux x86_64 | `linux-x64` |
| `linux-aarch64` | Linux ARM64 | `linux-arm64` |
| `windows-x64` | Windows x86_64 | `windows-x64` |
| `mac-x64` | macOS Intel | `osx-x64` |
| `mac-aarch64` | macOS Apple Silicon | `osx-arm64` |

Verify **SHA-256** and **size** from the manifest before extracting. Server re-checks integrity on serve.

**Suggested launcher logic:**

```
1. GET /java/{mcVersion} → major_version, optional temurin path
2. If temurin present:
     GET /java/temurin/{major}
     Pick platforms[] entry where runtimeOS matches host
     GET entry.url
     Verify SHA-256(entry.checksum.hash) and size
     Extract archive to local Java runtime dir
3. Else fall back to Mojang component / user Java / Adoptium directly
```

Prism Launcher-shaped fields: `runtimeOS`, `packageType`, `downloadType`, `checksum.type` = `sha-256`.

## Client implementation: Fabric

### Fabric Meta API (direct)

Replace `https://meta.fabricmc.net/v2/` with `{BASE}/fabric/v2/`:

```http
GET /fabric/v2/versions/loader
GET /fabric/v2/versions/loader/{gameVersion}
GET /fabric/v2/versions/loader/{gameVersion}/{loaderVersion}
```

Library URLs inside JSON are rewritten to `{BASE}/maven/fabricmc/...` when served.

### Fabric Maven

Replace `https://maven.fabricmc.net/` with `{BASE}/maven/fabricmc/`:

```http
GET /maven/fabricmc/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar
```

### PolyMC / Prism offline meta

Replace `https://meta.polymc.org/v1/` or `https://meta.prismlauncher.org/v1/` with `{BASE}/meta/v1/`:

```http
GET /meta/v1/index.json
GET /meta/v1/net.minecraft/index.json
GET /meta/v1/net.minecraft/{mcVersion}.json
GET /meta/v1/org.lwjgl3/index.json
GET /meta/v1/org.lwjgl3/{version}.json
GET /meta/v1/org.lwjgl/index.json
GET /meta/v1/org.lwjgl/{version}.json
GET /meta/v1/net.minecraft.java/index.json
GET /meta/v1/net.minecraft.java/java{major}.json
GET /meta/v1/net.fabricmc.fabric-loader/index.json?minecraft={mcVersion}
GET /meta/v1/net.fabricmc.fabric-loader/{loaderVersion}.json?minecraft={mcVersion}
GET /meta/v1/net.fabricmc.intermediary/index.json
GET /meta/v1/net.fabricmc.intermediary/{mcVersion}.json
```

Query `?minecraft={mcVersion}` scopes loader lists to one game version.

| Component | Source |
|-----------|--------|
| `net.minecraft` | Converted from archived Mojang version JSONs. `mainJar`, `assetIndex`, `libraries`, `compatibleJavaMajors`, `compatibleJavaName`, `minecraftArguments`/`+traits` derived per version. LWJGL entries move into `requires` like upstream meta. |
| `org.lwjgl3` / `org.lwjgl` | Generated from the LWJGL library entries in archived version JSONs. Natives classifiers flatten into `artifact-natives-{os}` names like upstream meta. |
| `net.minecraft.java` | Generated from archived Temurin manifests (`mva archive-java`). `runtimes[]` entries carry `downloadType: "archive"`, `packageType: "jdk"`, `sha256` checksums, and Prism-style `runtimeOS` values pointing at this mirror. |
| `net.fabricmc.fabric-loader` / `intermediary` | Generated from archived Fabric meta. |

`/meta/v1/` (no path) also serves the top-level index. Component indexes include `sha256` of each version file where the file is generated. Components with no archived data are omitted from the index and return 404 (or proxy to upstream meta when `proxy_on_miss` is on).

## Integrity and deduplication

| Asset type | Verification |
|------------|--------------|
| Libraries, jars | Mojang `sha1` + `size` in version JSON |
| Asset objects | SHA-1 in URL path |
| Temurin archives | SHA-256 + `size` in `/java/temurin/{major}` manifest |

Clients should always verify. The mirror also verifies on serve for assets and Temurin.

## Optional server behaviors

| Setting | Client impact |
|---------|---------------|
| `proxy_on_miss` | **404** may become a one-time upstream fetch; file may appear later if cached |
| `trust_proxy` | JSON URLs use `X-Forwarded-Proto` + `X-Forwarded-Host` when present |
| `stats.enabled` | Exposes `GET /stats` (JSON metrics) |
| `block_scanners` | Non-GET/HEAD (except `/p2p/`) → connection drop |

## Operations endpoints

### Health

```http
GET /health
```

```json
{"status":"ok"}
```

### Stats (when enabled)

```http
GET /stats
```

```json
{
  "uptime_sec": 3600,
  "total_requests": 12000,
  "total_bytes_served": 50000000000,
  "total_bytes_in": 1000000,
  "blocked_requests": 42,
  "cache_hits": 8000,
  "cache_misses": 4000,
  "serve_bytes_per_sec": 1500000,
  "versions_stored": 85
}
```

### P2P inventory (federation)

```http
GET /p2p/inventory
```

```json
{
  "versions": ["1.21.1", "1.20.1"],
  "libraries": ["com/mojang/authlib/..."],
  "assets": ["abcdef0123..."]
}
```

Used by other `mva` archivers, not typical launchers.

## Upstream mapping cheat sheet

| Upstream | Mirror path |
|----------|-------------|
| `https://piston-meta.mojang.com/mc/game/version_manifest_v2.json` | `/mc/game/version_manifest_v2.json` |
| `https://piston-meta.mojang.com/v1/packages/.../...json` | `/versions/{id}.json` |
| `https://piston-data.mojang.com/.../client.jar` | `/jars/client/{id}.jar` |
| `https://libraries.minecraft.net/{path}` | `/libraries/{path}` |
| `https://resources.download.minecraft.net/{aa}/{hash}` | `/assets/objects/{aa}/{hash}` |
| `https://meta.fabricmc.net/v2/{path}` | `/fabric/v2/{path}` |
| `https://maven.fabricmc.net/{path}` | `/maven/fabricmc/{path}` |
| `https://meta.polymc.org/v1/{path}` | `/meta/v1/{path}` |
| Adoptium / Temurin (operator-archived) | `/java/temurin/...` |

## Agent checklist

When implementing a new client against this API:

1. Set `{BASE}` once; fetch `GET /` to confirm `public_url`.
2. Use manifest → version JSON → artifact URLs **as returned** (already rewritten).
3. Honor **SHA-1** for game files, **SHA-256** for Temurin.
4. Support **HTTP Range** for large jar downloads and resume.
5. Handle **404** as "not on this mirror" (version not archived or file missing).
6. Use `/java/{mcVersion}` + `/java/temurin/{major}` for auto-Java when Temurin is hosted.
7. For Fabric, swap only the meta/maven host prefixes; JSON shapes are unchanged.
8. Apply version aliases (`1.5.0` → `1.5`) when resolving user input.
9. Do not send POST/PUT to game paths; use GET/HEAD only.

## Related CLI (not HTTP)

Operators populate the mirror with:

```bash
mva archive [version...]       # Mojang game files
mva archive-fabric [...]       # Fabric meta + maven
mva archive-java [...]         # Eclipse Temurin JDKs
mva serve                      # start this HTTP API
```

See `README.md` for configuration (`settings.ini`, `MVA_*` env vars).
