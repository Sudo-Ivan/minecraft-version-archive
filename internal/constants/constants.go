package constants

import "time"

const (
	EnvPrefix = "MVA_"

	DefaultDataDir      = "./data"
	DefaultListen       = ":8080"
	DefaultPublic       = "http://localhost:8080"
	DefaultSettingsFile = "settings.ini"

	DefaultWorkers     = 8
	DefaultRetryMax    = 5
	DefaultRetryBaseMS = 500
	DefaultP2PTimeout  = 30

	DefaultCacheEntries = 512
	DefaultCacheMB      = 512
	DefaultCacheShards  = 256
	DefaultInventoryTTL = 30

	LRUTouchInterval = 8
	MinShardCapacity = 4

	DirManifest    = "manifest"
	DirVersions    = "versions"
	DirClient      = "client"
	DirServer      = "server"
	DirLibraries   = "libraries"
	DirAssets      = "assets"
	DirIndexes     = "indexes"
	DirObjects     = "objects"
	DirJars        = "jars"
	DirInventory   = "inventory"
	DirCoordinator = "coordinator"
	DirClaims      = "claims"
	DirDone        = "done"
	DirFabric      = "fabric"
	DirFabricMeta  = "meta"
	DirFabricMaven = "maven"
	DirJava        = "java"
	DirTemurin     = "temurin"

	DefaultCoordinatorStale = 3600

	FileManifest    = "version_manifest_v2.json"
	FilePartSuffix  = ".part"
	FileVersionJSON = ".json"
	FileClientJar   = ".jar"
	FileHashIndex   = "sha1_index.json"

	RouteRoot           = "GET /{$}"
	RouteHealth         = "GET /health"
	RouteStats          = "GET /stats"
	RouteInventory      = "GET /p2p/inventory"
	RouteManifest       = "GET /mc/game/version_manifest_v2.json"
	RouteJavaIndex      = "GET /java"
	RouteJavaVersion    = "GET /java/{id}"
	RouteJavaTemurinIdx = "GET /java/temurin"
	RouteJavaTemurin    = "GET /java/temurin/{major}"
	RouteJavaTemurinBin = "GET /java/temurin/{major}/{platform}/{file}"
	RouteVersions       = "GET /versions/{file}"
	RouteJars           = "GET /jars/{kind}/{file}"
	RouteLibraries      = "GET /libraries/{path...}"
	RouteAssetIdx       = "GET /assets/indexes/{file}"
	RouteAssetObj       = "GET /assets/objects/{prefix}/{hash}"
	RouteFabricMeta     = "GET /fabric/v2/{path...}"
	RouteFabricMaven    = "GET /maven/fabricmc/{path...}"
	RoutePolyMCMeta     = "GET /meta/v1/{path...}"

	HeaderAcceptRanges    = "Accept-Ranges"
	HeaderCacheControl    = "Cache-Control"
	HeaderContentLength   = "Content-Length"
	HeaderContentRange    = "Content-Range"
	HeaderContentType     = "Content-Type"
	HeaderETag            = "ETag"
	HeaderIfNoneMatch     = "If-None-Match"
	HeaderRange           = "Range"
	HeaderAcceptEncoding  = "Accept-Encoding"
	HeaderContentEncoding = "Content-Encoding"
	HeaderVary            = "Vary"
	HeaderBytes           = "bytes"
	HeaderBytesPrefix     = "bytes="

	CacheControlImmutable = "public, max-age=31536000, immutable"
	CacheControlShort     = "public, max-age=60"
	CacheControlNoStore   = "no-store"

	MIMEJSON = "application/json"

	ReadHeaderTimeout   = 10 * time.Second
	IdleTimeout         = 120 * time.Second
	ShutdownTimeout     = 15 * time.Second
	FetchTimeout        = 10 * time.Minute
	StatsWindow         = 10 * time.Second
	StatsRingSize       = 128
	StatsStdoutInterval = 15 * time.Minute

	CacheServeFraction = 4

	MaxGetBodyBytes        = 64 << 20
	StatsMaxTrackedClients = 1 << 17
	VerifyCacheMaxEntries  = 1 << 18
	StatsHistoryMaxBytes   = 1 << 20

	// GzipMinBytes is the smallest JSON body worth compressing.
	GzipMinBytes = 256

	INISectionGeneral = "general"
	INISectionArchive = "archive"
	INISectionCache   = "cache"
	INISectionServer  = "server"
	INISectionStats   = "stats"
	INISectionP2P     = "p2p"
)

var (
	MojangManifest  = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	MojangMetaBase  = "https://piston-meta.mojang.com"
	MojangLibBase   = "https://libraries.minecraft.net"
	MojangAssetBase = "https://resources.download.minecraft.net"
	MojangLibHost   = "libraries.minecraft.net/"

	FabricMetaBase  = "https://meta.fabricmc.net"
	FabricMavenBase = "https://maven.fabricmc.net"
)

var (
	HealthBody      = []byte(`{"status":"ok"}`)
	AcceptRanges    = []string{"bytes"}
	ContentTypeJSON = []string{MIMEJSON}
)
