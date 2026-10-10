package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"minecraft-version-archive/internal/archive"
	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/humanlog"
	"minecraft-version-archive/internal/server"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

const usageText = `mva - Minecraft version archive and mirror

Usage:
  mva [command]

Commands:
  serve     Start HTTP server (default); optional --host, --port, --public-url
  archive   Download versions from Mojang; optional VERSION or --version
  archive-fabric  Download Fabric loader metadata and libraries for archived MC versions
  archive-java    Download Eclipse Temurin JDKs for required Java major versions
  export    Zip the data folder for backup or transfer
  import    Restore a data folder from a zip
  version   Print version
  help      Show this help

No config file required. Defaults apply when settings.ini is absent.

Quick start:
  mva                 Serve ./data on :8080
  mva archive         Archive latest release, then serve
  mva archive && mva serve

Fabric (after vanilla versions are archived):
  mva archive-fabric
  mva archive-fabric 1.21.1
  mva archive-fabric --all-loaders 1.20.1

Java (Eclipse Temurin, after vanilla versions are archived):
  mva archive-java
  mva archive-java 21
  mva archive-java --platform linux-x64,windows-x64 17 21

Optional:
  -settings path      Settings file (auto-detects settings.ini if present)

Environment: MVA_DATA_DIR, MVA_LISTEN, MVA_VERSIONS, MVA_ARCHIVE_ALL, ...
`

const archiveUsageText = `Usage: mva archive [options] [version...]

Download Minecraft versions from Mojang into data_dir.

With no version arguments, archives the latest release.
--all archives every release by default; add include flags for other types.

Options:
  -version string         Version to archive (repeatable)
  -all                    Archive all matching versions (releases by default)
  -include-snapshots      Include weekly and numbered snapshots
  -include-prereleases    Include pre-releases (e.g. 26.1-pre-1)
  -include-rc             Include release candidates
  -include-alpha          Include old_alpha versions
  -include-beta           Include old_beta versions
  -include-all-types      Include snapshots, prereleases, rc, alpha, and beta
  -versions-file path     Read version IDs from a file (one per line)

Examples:
  mva archive
  mva archive --all
  mva archive --all --include-snapshots --include-prereleases
  mva archive --all --include-all-types
  mva archive 1.20.1
`

const archiveFabricUsageText = `Usage: mva archive-fabric [options] [minecraft-version...]

Download Fabric loader metadata and Maven artifacts for archived Minecraft versions.
With no version arguments, uses every version already present in data/versions/.

Options:
  -version string      Minecraft version (repeatable)
  -loader string       Fabric loader version (default: latest stable per MC version)
  -all-loaders         Archive every compatible loader, not just the recommended one
  -h, -help            Show this help

Examples:
  mva archive-fabric
  mva archive-fabric 1.21.1
  mva archive-fabric --loader 0.16.9 1.20.1
  mva archive-fabric --all-loaders 1.21.1
`

const archiveJavaUsageText = `Usage: mva archive-java [options] [major-version...]

Download Eclipse Temurin JDK archives for Java major versions required by archived
Minecraft versions. Each binary is verified with SHA-256 before it is kept.

With no major-version arguments, derives majors from javaVersion fields in
data/versions/*.json.

Options:
  -platform string   Comma-separated platforms (default: all supported)
  -h, -help         Show this help

Platforms: linux-x64, linux-aarch64, windows-x64, mac-x64, mac-aarch64

Examples:
  mva archive-java
  mva archive-java 21
  mva archive-java --platform linux-x64,windows-x64 17 21
`

const exportUsageText = `Usage: mva export [options] [file.zip]

Create a zip backup of data_dir. Coordinator claims are excluded.

With no version arguments, exports the full data folder.
With version arguments, exports only those versions and their dependencies.

Options:
  -output path         Output zip path (default: data-export.zip)
  -version string      Version to include (repeatable)
  -versions-file path  Read version IDs from a file

Examples:
  mva export
  mva export backup.zip
  mva export -output partial.zip -version 1.20.1 -version 1.19.4
  mva export releases.zip --version 1.21.11 --version 1.20.1
`

const importUsageText = `Usage: mva import [options] file.zip

Restore data_dir from a zip created by mva export.

Options:
  -replace       Clear data_dir before importing

Examples:
  mva import backup.zip
  mva import -replace backup.zip
`

var errHelp = errors.New("help")

func main() {
	settings := flag.String("settings", "", "settings.ini path (optional, auto-detected)")
	flag.Parse()

	log := slog.New(humanlog.New(os.Stderr))

	cfg, err := config.Load(*settings)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := "serve"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}

	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, usageText)
		return
	}

	archiver := archive.NewWithStats(cfg, log, stats.New())

	switch cmd {
	case "archive":
		if err := parseArchiveArgs(cfg, flag.Args()[1:]); err != nil {
			if errors.Is(err, errHelp) {
				return
			}
			log.Error("archive", "err", err)
			os.Exit(1)
		}
		if err := archiver.Run(ctx); err != nil {
			log.Error("archive", "err", err)
			os.Exit(1)
		}
	case "archive-fabric":
		fabricOpts, err := parseArchiveFabricArgs(flag.Args()[1:])
		if err != nil {
			if errors.Is(err, errHelp) {
				return
			}
			log.Error("archive-fabric", "err", err)
			os.Exit(1)
		}
		if err := archiver.RunFabric(ctx, fabricOpts); err != nil {
			log.Error("archive-fabric", "err", err)
			os.Exit(1)
		}
	case "archive-java":
		javaOpts, err := parseArchiveJavaArgs(flag.Args()[1:])
		if err != nil {
			if errors.Is(err, errHelp) {
				return
			}
			log.Error("archive-java", "err", err)
			os.Exit(1)
		}
		if err := archiver.RunJava(ctx, javaOpts); err != nil {
			log.Error("archive-java", "err", err)
			os.Exit(1)
		}
	case "export":
		if err := runExport(cfg, log, flag.Args()[1:]); err != nil {
			if errors.Is(err, errHelp) {
				return
			}
			log.Error("export", "err", err)
			os.Exit(1)
		}
	case "import":
		if err := runImport(cfg, log, flag.Args()[1:]); err != nil {
			if errors.Is(err, errHelp) {
				return
			}
			log.Error("import", "err", err)
			os.Exit(1)
		}
	case "serve":
		if err := parseServeArgs(cfg, flag.Args()[1:]); err != nil {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
		layout := store.New(cfg.DataDir)
		if err := layout.Ensure(); err != nil {
			log.Error("data dir", "err", err)
			os.Exit(1)
		}
		if n := layout.CleanStaleParts(time.Duration(cfg.CoordinatorStaleSec) * time.Second); n > 0 {
			log.Info("removed stale partial downloads", "count", n)
		}
		log.Info("starting server", "data", cfg.DataDir, "listen", cfg.Listen, "public", cfg.PublicURL, "block_scanners", cfg.BlockScanners, "stats_stdout", cfg.StatsStdout)
		srv := server.New(cfg, archiver, log)
		if cfg.StatsStdout {
			srv.StartPeriodicStats(ctx, os.Stdout, constants.StatsStdoutInterval)
		}
		go func() {
			<-ctx.Done()
			log.Info("shutting down")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), constants.ShutdownTimeout)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	case "version":
		println("mva dev")
	default:
		log.Error("unknown command", "cmd", cmd, "hint", "run mva help")
		os.Exit(1)
	}
}

func parseServeArgs(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	host := fs.String("host", "", "listen host (default: all interfaces)")
	port := fs.Int("port", 0, "listen port")
	publicURL := fs.String("public-url", "", "public base URL for rewritten links (e.g. https://mc.example.com)")
	trustProxy := fs.Bool("trust-proxy", false, "use X-Forwarded-Proto/Host behind a reverse proxy")
	proxyOnMiss := fs.Bool("proxy-on-miss", false, "fetch missing files from Mojang on request")
	noProxyCache := fs.Bool("no-proxy-cache", false, "do not save proxied files to data_dir")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	positional := ""
	if fs.NArg() == 1 {
		positional = fs.Arg(0)
	}
	if *trustProxy {
		cfg.TrustProxy = true
	}
	if *proxyOnMiss {
		cfg.ProxyOnMiss = true
	}
	if *noProxyCache {
		cfg.ProxyCache = false
	}
	return cfg.ApplyServeOverrides(*host, *port, positional, *publicURL)
}

type versionListFlag []string

func (v *versionListFlag) String() string {
	return strings.Join(*v, ", ")
}

func (v *versionListFlag) Set(s string) error {
	*v = append(*v, s)
	return nil
}

func parseArchiveArgs(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "show help")
	fs.BoolVar(&showHelp, "help", false, "show help")

	all := fs.Bool("all", false, "archive all matching versions (releases by default)")
	includeSnapshots := fs.Bool("include-snapshots", false, "include weekly and numbered snapshots")
	includePreReleases := fs.Bool("include-prereleases", false, "include pre-releases")
	includeRC := fs.Bool("include-rc", false, "include release candidates")
	includeAlpha := fs.Bool("include-alpha", false, "include old_alpha versions")
	includeBeta := fs.Bool("include-beta", false, "include old_beta versions")
	includeAllTypes := fs.Bool("include-all-types", false, "include snapshots, prereleases, rc, alpha, and beta")
	versionsFile := fs.String("versions-file", "", "read version IDs from a file")
	var versions versionListFlag
	fs.Var(&versions, "version", "version to archive (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showHelp {
		fmt.Fprint(os.Stdout, archiveUsageText)
		return errHelp
	}

	selected := append(append([]string{}, versions...), fs.Args()...)
	return cfg.ApplyArchiveCLI(config.ArchiveCLIInput{
		All:          *all,
		VersionsFile: *versionsFile,
		Versions:     selected,
		Includes: config.ArchiveIncludes{
			Snapshots:   *includeSnapshots,
			PreReleases: *includePreReleases,
			RC:          *includeRC,
			Alpha:       *includeAlpha,
			Beta:        *includeBeta,
		},
		IncludeAll: *includeAllTypes,
	})
}

func parseArchiveFabricArgs(args []string) (archive.FabricOptions, error) {
	fs := flag.NewFlagSet("archive-fabric", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "show help")
	fs.BoolVar(&showHelp, "help", false, "show help")
	allLoaders := fs.Bool("all-loaders", false, "archive all compatible loaders")
	loader := fs.String("loader", "", "fabric loader version")
	var versions versionListFlag
	fs.Var(&versions, "version", "minecraft version (repeatable)")
	if err := fs.Parse(args); err != nil {
		return archive.FabricOptions{}, err
	}
	if showHelp {
		fmt.Fprint(os.Stdout, archiveFabricUsageText)
		return archive.FabricOptions{}, errHelp
	}
	selected := append([]string{}, versions...)
	selected = append(selected, fs.Args()...)
	return archive.FabricOptions{
		MCVersions:    selected,
		LoaderVersion: *loader,
		AllLoaders:    *allLoaders,
	}, nil
}

func parseArchiveJavaArgs(args []string) (archive.JavaOptions, error) {
	fs := flag.NewFlagSet("archive-java", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "show help")
	fs.BoolVar(&showHelp, "help", false, "show help")
	platforms := fs.String("platform", "", "comma-separated platforms")
	if err := fs.Parse(args); err != nil {
		return archive.JavaOptions{}, err
	}
	if showHelp {
		fmt.Fprint(os.Stdout, archiveJavaUsageText)
		return archive.JavaOptions{}, errHelp
	}
	var platList []string
	if *platforms != "" {
		for p := range strings.SplitSeq(*platforms, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				platList = append(platList, p)
			}
		}
	}
	var majors []int
	for _, arg := range fs.Args() {
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 {
			return archive.JavaOptions{}, fmt.Errorf("invalid java major version %q", arg)
		}
		majors = append(majors, n)
	}
	return archive.JavaOptions{
		Majors:    majors,
		Platforms: platList,
	}, nil
}

func runExport(cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "show help")
	fs.BoolVar(&showHelp, "help", false, "show help")
	output := fs.String("output", "", "output zip path")
	versionsFile := fs.String("versions-file", "", "read version IDs from a file")
	var versions versionListFlag
	fs.Var(&versions, "version", "version to include (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if showHelp {
		fmt.Fprint(os.Stdout, exportUsageText)
		return errHelp
	}
	dest := *output
	positional := fs.Args()
	if dest == "" && len(positional) == 1 && strings.HasSuffix(strings.ToLower(positional[0]), ".zip") {
		dest = positional[0]
		positional = nil
	}
	if dest == "" {
		dest = "data-export.zip"
	}
	if len(positional) > 0 {
		versions = append(versions, positional...)
	}
	selected := append([]string{}, versions...)
	if *versionsFile != "" {
		ids, err := config.ReadVersionList(*versionsFile)
		if err != nil {
			return err
		}
		selected = append(selected, ids...)
	}
	log.Info("export started", "source", cfg.DataDir, "dest", dest, "versions", len(selected))
	result, err := store.ExportZip(cfg.DataDir, dest, store.ExportOptions{Versions: selected})
	if err != nil {
		return err
	}
	log.Info("export complete", "dest", dest, "files", result.Files, "bytes", result.Bytes)
	return nil
}

func runImport(cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	var showHelp bool
	fs.BoolVar(&showHelp, "h", false, "show help")
	fs.BoolVar(&showHelp, "help", false, "show help")
	replace := fs.Bool("replace", false, "clear data_dir before importing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if showHelp {
		fmt.Fprint(os.Stdout, importUsageText)
		return errHelp
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: mva import file.zip")
	}
	zipPath := fs.Arg(0)
	log.Info("import started", "source", zipPath, "dest", cfg.DataDir, "replace", *replace)
	result, err := store.ImportZip(cfg.DataDir, zipPath, *replace)
	if err != nil {
		return err
	}
	log.Info("import complete", "dest", cfg.DataDir, "files", result.Files, "bytes", result.Bytes)
	return nil
}
