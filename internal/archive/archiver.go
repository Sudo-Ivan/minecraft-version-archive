package archive

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/coordinator"
	"minecraft-version-archive/internal/fetch"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/mojang"
	"minecraft-version-archive/internal/p2p"
	"minecraft-version-archive/internal/stats"
	"minecraft-version-archive/internal/store"
)

type Archiver struct {
	cfg      *config.Config
	layout   *store.Layout
	index    *store.HashIndex
	mojang   *mojang.Client
	fetch    *fetch.Client
	peers    *p2p.Registry
	log      *slog.Logger
	manifest *mojang.Manifest
	stats    *stats.Collector
}

func New(cfg *config.Config, log *slog.Logger) *Archiver {
	return NewWithStats(cfg, log, nil)
}

func NewWithStats(cfg *config.Config, log *slog.Logger, st *stats.Collector) *Archiver {
	var peers *p2p.Registry
	if cfg.P2PEnabled && len(cfg.Peers) > 0 {
		peers = p2p.NewRegistry(cfg.Peers, cfg.P2PTimeout, cfg.RetryMax, cfg.RetryBaseMS)
	}
	layout := store.New(cfg.DataDir)
	fetchClient := fetch.New(cfg.RetryMax, cfg.RetryBaseMS)
	if st != nil {
		fetchClient = fetchClient.WithStats(st)
	}
	if dir, err := layout.Dir(); err == nil {
		fetchClient = fetchClient.WithDataDir(dir)
	}
	mojangClient := mojang.NewClientWithFetch(fetchClient)
	return &Archiver{
		cfg:    cfg,
		layout: store.New(cfg.DataDir),
		mojang: mojangClient,
		fetch:  fetchClient,
		peers:  peers,
		log:    log,
		stats:  st,
	}
}

func (a *Archiver) Run(ctx context.Context) error {
	if err := a.layout.Ensure(); err != nil {
		return err
	}

	index, err := store.OpenHashIndex(a.layout)
	if err != nil {
		return fmt.Errorf("hash index: %w", err)
	}
	a.index = index

	manifest, err := a.mojang.FetchManifest(ctx)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}
	a.manifest = manifest

	ids, err := a.resolveIDs(manifest)
	if err != nil {
		return err
	}

	coord, err := coordinator.New(a.layout.CoordinatorDir(), a.cfg.AgentID, a.cfg.CoordinatorStaleSec)
	if err != nil {
		return err
	}

	pending := coordinator.Pending(ids, coord)
	if len(pending) == 0 {
		a.log.Info("archive up to date", "versions", len(ids))
		return a.writeManifest(manifest, ids)
	}

	a.log.Info("archive plan",
		"agent", a.cfg.AgentID,
		"workers", a.cfg.Workers,
		"pending", len(pending),
		"total", len(ids),
		"skipped", len(ids)-len(pending),
	)

	prog := NewProgress(len(pending), a.log)
	go prog.Run(ctx)

	var wg sync.WaitGroup
	errCh := make(chan error, len(pending))
	var once sync.Once
	var firstErr error

	for i := 0; i < a.cfg.Workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				id, ok := coord.ClaimNext(ids)
				if !ok {
					return
				}
				prog.BeginVersion(id)
				a.log.Info("claimed version", "version", id, "worker", worker, "agent", a.cfg.AgentID)
				err := a.archiveVersion(ctx, id, prog)
				if err != nil {
					coord.Release(id)
					prog.EndVersion(id, false)
					errCh <- fmt.Errorf("%s: %w", id, err)
					continue
				}
				if err := coord.MarkDone(id); err != nil {
					coord.Release(id)
					prog.EndVersion(id, false)
					errCh <- fmt.Errorf("%s: mark done: %w", id, err)
					continue
				}
				coord.Release(id)
				prog.EndVersion(id, true)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	prog.logStatus(true)

	for err := range errCh {
		a.log.Error("archive failed", "err", err)
		once.Do(func() { firstErr = err })
	}

	if err := a.writeManifest(manifest, ids); err != nil {
		return err
	}

	if firstErr != nil {
		return fmt.Errorf("archive finished with errors: %w", firstErr)
	}
	a.log.Info("archive complete", "versions", len(pending))
	return nil
}

func (a *Archiver) writeManifest(manifest *mojang.Manifest, ids []string) error {
	rewritten := mojang.RewriteManifest(manifest, a.cfg.PublicURL)
	archived := a.filterArchivedVersions(rewritten, ids)
	if err := store.WriteJSON(a.layout, a.layout.ManifestPath(), archived); err != nil {
		return err
	}
	a.log.Info("manifest written", "path", a.layout.ManifestPath(), "versions", len(archived.Versions))
	return nil
}

func (a *Archiver) resolveIDs(manifest *mojang.Manifest) ([]string, error) {
	if a.cfg.ArchiveAll {
		ids := mojang.FilterVersionIDs(manifest.Versions, a.cfg.AllowsVersionChannel)
		if len(ids) == 0 {
			return nil, fmt.Errorf("no versions matched include filters")
		}
		counts := mojang.CountByChannel(manifest.Versions)
		selected := mojang.CountByChannel(filteredVersions(manifest.Versions, ids))
		a.log.Info("archiving filtered versions",
			"total", len(ids),
			"releases", selected["release"],
			"snapshots", selected["snapshot"],
			"prereleases", selected["prerelease"],
			"rc", selected["rc"],
			"alpha", selected["alpha"],
			"beta", selected["beta"],
			"manifest_total", len(manifest.Versions),
			"manifest_releases", counts["release"],
			"manifest_snapshots", counts["snapshot"],
			"manifest_prereleases", counts["prerelease"],
			"manifest_rc", counts["rc"],
			"manifest_alpha", counts["alpha"],
			"manifest_beta", counts["beta"],
		)
		return ids, nil
	}
	if !a.cfg.HasExplicitArchiveSelection() {
		if manifest.Latest.Release == "" {
			return nil, fmt.Errorf("manifest has no latest release")
		}
		a.log.Info("archiving latest release", "version", manifest.Latest.Release)
		return []string{manifest.Latest.Release}, nil
	}
	requested, err := a.cfg.ResolveVersionIDs()
	if err != nil {
		return nil, err
	}
	a.log.Info("archiving versions", "versions", requested)
	ids, err := mojang.ResolveVersionIDs(requested, manifest.Versions)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func filteredVersions(versions []mojang.ManifestVersion, ids []string) []mojang.ManifestVersion {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	out := make([]mojang.ManifestVersion, 0, len(ids))
	for _, v := range versions {
		if _, ok := set[v.ID]; ok {
			out = append(out, v)
		}
	}
	return out
}

func (a *Archiver) filterArchivedVersions(m *mojang.Manifest, ids []string) *mojang.Manifest {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	out := *m
	out.Versions = make([]mojang.ManifestVersion, 0, len(ids))
	for _, v := range m.Versions {
		if _, ok := set[v.ID]; ok {
			out.Versions = append(out.Versions, v)
		}
	}
	return &out
}

func (a *Archiver) archiveVersion(ctx context.Context, id string, prog *Progress) error {
	mv := a.findManifestVersion(id)
	if mv == nil {
		return fmt.Errorf("not in manifest")
	}

	versionPath := a.layout.VersionJSON(id)
	version, raw, err := a.loadVersionJSON(ctx, id, mv.URL, versionPath)
	if err != nil {
		return err
	}
	if mv.SHA1 != "" {
		if err := integrity.VerifyBytes(raw, integrity.Spec{SHA1: mv.SHA1}); err != nil {
			return fmt.Errorf("version json: %w", err)
		}
	}

	prog.AddTasks(a.estimateTasks(version))

	rewritten, err := mojang.RewriteVersionJSON(raw, a.cfg.PublicURL, id)
	if err != nil {
		return err
	}
	if err := store.WriteBytes(a.layout, versionPath, rewritten); err != nil {
		return err
	}
	prog.TaskDone(int64(len(rewritten)))

	if !a.cfg.ServerOnly {
		if err := a.archiveClient(ctx, id, version, prog); err != nil {
			return err
		}
		if err := a.archiveAssets(ctx, version, prog); err != nil {
			return err
		}
		if err := a.archiveLibraries(ctx, version, prog); err != nil {
			return err
		}
	}

	if !a.cfg.ClientOnly {
		if err := a.archiveServer(ctx, id, version, prog); err != nil {
			return err
		}
	}

	return nil
}

func (a *Archiver) estimateTasks(v *mojang.Version) int64 {
	var n int64 = 1
	if !a.cfg.ServerOnly {
		if v.Downloads.Client != nil {
			n++
		}
		if v.AssetIndex.URL != "" {
			n++
		}
		for range v.Libraries {
			n++
		}
	}
	if !a.cfg.ClientOnly && v.Downloads.Server != nil {
		n++
	}
	return n
}

func (a *Archiver) archiveClient(ctx context.Context, id string, v *mojang.Version, prog *Progress) error {
	if v.Downloads.Client == nil {
		return nil
	}
	dest := a.layout.ClientJar(id)
	return a.ensureArtifact(ctx, v.Downloads.Client.URL, dest, integrity.FromArtifact(v.Downloads.Client.SHA1, v.Downloads.Client.Size), func() bool {
		return a.tryPeerClient(ctx, id, dest)
	}, prog)
}

func (a *Archiver) archiveServer(ctx context.Context, id string, v *mojang.Version, prog *Progress) error {
	if v.Downloads.Server == nil {
		a.log.Debug("no server jar", "version", id)
		prog.TaskDone(0)
		return nil
	}
	dest := a.layout.ServerJar(id)
	return a.ensureArtifact(ctx, v.Downloads.Server.URL, dest, integrity.FromArtifact(v.Downloads.Server.SHA1, v.Downloads.Server.Size), func() bool {
		return a.tryPeerServer(ctx, id, dest)
	}, prog)
}

func (a *Archiver) archiveLibraries(ctx context.Context, v *mojang.Version, prog *Progress) error {
	type job struct {
		url  string
		dest string
		path string
		spec integrity.Spec
	}
	var jobs []job
	for _, lib := range v.Libraries {
		artifactPath := mojang.LibraryArtifactPath(lib)
		if artifactPath == "" || lib.Downloads.Artifact == nil {
			continue
		}
		dest := a.layout.LibraryPath(artifactPath)
		spec := integrity.FromArtifact(lib.Downloads.Artifact.SHA1, lib.Downloads.Artifact.Size)
		if a.resolveFromIndex(dest, spec, prog) {
			continue
		}
		if a.layout.Exists(dest) {
			if err := a.verifyFile(dest, spec); err == nil {
				a.index.Record(spec, dest)
				prog.TaskDone(fileSize(dest))
				continue
			}
			_ = os.Remove(dest)
		}
		jobs = append(jobs, job{
			url:  lib.Downloads.Artifact.URL,
			dest: dest,
			path: artifactPath,
			spec: spec,
		})
	}
	if len(jobs) == 0 {
		return nil
	}

	sem := make(chan struct{}, a.cfg.Workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, j := range jobs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if a.tryPeerLibrary(ctx, j.path, j.dest) {
				if err := a.verifyFile(j.dest, j.spec); err != nil {
					_ = os.Remove(j.dest)
				} else {
					a.index.Record(j.spec, j.dest)
					prog.TaskDone(fileSize(j.dest))
					return
				}
			}
			if err := a.fetch.DownloadVerified(ctx, j.url, j.dest, j.spec); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("library %s: %w", j.path, err)
				}
				mu.Unlock()
				return
			}
			prog.TaskDone(fileSize(j.dest))
			a.index.Record(j.spec, j.dest)
		})
	}
	wg.Wait()
	return firstErr
}

func (a *Archiver) archiveAssets(ctx context.Context, v *mojang.Version, prog *Progress) error {
	if v.AssetIndex.URL == "" {
		return nil
	}
	indexPath := a.layout.AssetIndexPath(v.AssetIndex.ID)
	indexSpec := integrity.FromArtifact(v.AssetIndex.SHA1, v.AssetIndex.Size)
	if err := a.ensureArtifact(ctx, v.AssetIndex.URL, indexPath, indexSpec, func() bool {
		return a.tryPeerAssetIndex(ctx, v.AssetIndex.ID, indexPath)
	}, prog); err != nil {
		return fmt.Errorf("asset index: %w", err)
	}

	idx, err := decodeAssetIndexFile(a.layout, indexPath)
	if err != nil {
		idx, err = a.mojang.FetchAssetIndex(ctx, v.AssetIndex.URL)
		if err != nil {
			return err
		}
	}
	prog.AddTasks(int64(len(idx.Objects)))

	sem := make(chan struct{}, a.cfg.Workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, obj := range idx.Objects {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			dest := a.layout.AssetObjectPath(obj.Hash)
			spec := integrity.Spec{SHA1: obj.Hash, Size: obj.Size}
			if a.resolveFromIndex(dest, spec, prog) {
				return
			}
			if a.layout.Exists(dest) {
				if err := a.verifyFile(dest, spec); err == nil {
					a.index.Record(spec, dest)
					prog.TaskDone(0)
					return
				}
				_ = os.Remove(dest)
			}
			url := constants.MojangAssetBase + "/" + obj.Hash[:2] + "/" + obj.Hash
			if a.tryPeerAssetObject(ctx, obj.Hash, dest) {
				if err := a.verifyFile(dest, spec); err != nil {
					_ = os.Remove(dest)
				} else {
					a.index.Record(spec, dest)
					prog.TaskDone(fileSize(dest))
					return
				}
			}
			if err := a.fetch.DownloadVerified(ctx, url, dest, spec); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("asset %s: %w", obj.Hash, err)
				}
				mu.Unlock()
				return
			}
			prog.TaskDone(fileSize(dest))
			a.index.Record(spec, dest)
		})
	}
	wg.Wait()
	return firstErr
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func (a *Archiver) verifyFile(path string, spec integrity.Spec) error {
	dir, err := a.layout.Dir()
	if err != nil {
		return err
	}
	return integrity.VerifyFile(dir, path, spec)
}

func (a *Archiver) tryPeerClient(ctx context.Context, id, dest string) bool {
	if a.peers == nil {
		return false
	}
	peer, err := a.peers.FindVersion(ctx, id)
	if err != nil {
		return false
	}
	return a.peers.FetchClientJar(ctx, peer, id, dest) == nil
}

func (a *Archiver) tryPeerServer(ctx context.Context, id, dest string) bool {
	if a.peers == nil {
		return false
	}
	peer, err := a.peers.FindVersion(ctx, id)
	if err != nil {
		return false
	}
	return a.peers.FetchServerJar(ctx, peer, id, dest) == nil
}

func (a *Archiver) tryPeerLibrary(ctx context.Context, artifactPath, dest string) bool {
	if a.peers == nil || len(a.peers.Peers()) == 0 {
		return false
	}
	for _, peer := range a.peers.Peers() {
		if a.peers.FetchLibrary(ctx, peer, artifactPath, dest) == nil {
			return true
		}
	}
	return false
}

func (a *Archiver) tryPeerAssetIndex(ctx context.Context, id, dest string) bool {
	if a.peers == nil || len(a.peers.Peers()) == 0 {
		return false
	}
	for _, peer := range a.peers.Peers() {
		if a.peers.FetchAssetIndex(ctx, peer, id, dest) == nil {
			return true
		}
	}
	return false
}

func (a *Archiver) tryPeerAssetObject(ctx context.Context, hash, dest string) bool {
	if a.peers == nil || len(a.peers.Peers()) == 0 {
		return false
	}
	for _, peer := range a.peers.Peers() {
		if a.peers.FetchAssetObject(ctx, peer, hash, dest) == nil {
			return true
		}
	}
	return false
}

func (a *Archiver) findManifestVersion(id string) *mojang.ManifestVersion {
	if a.manifest == nil {
		return nil
	}
	for i := range a.manifest.Versions {
		if a.manifest.Versions[i].ID == id {
			return &a.manifest.Versions[i]
		}
	}
	return nil
}

func (a *Archiver) Stats() *stats.Collector {
	return a.stats
}

func (a *Archiver) VersionCount() int {
	return len(a.BuildInventory().Versions)
}

func (a *Archiver) BuildInventory() p2p.Inventory {
	inv := p2p.Inventory{}
	versionsDir := a.layout.VersionsDir()
	librariesDir := a.layout.LibrariesDir()
	objectsDir := a.layout.AssetObjectsDir()
	_ = filepath.Walk(a.layout.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		inv.Bytes += info.Size()
		switch dir := filepath.Dir(path); {
		case dir == versionsDir && filepath.Ext(info.Name()) == ".json":
			name := info.Name()
			inv.Versions = append(inv.Versions, name[:len(name)-5])
		case strings.HasPrefix(path, librariesDir+string(filepath.Separator)):
			rel, err := filepath.Rel(librariesDir, path)
			if err == nil {
				inv.Libraries = append(inv.Libraries, filepath.ToSlash(rel))
			}
		case strings.HasPrefix(path, objectsDir+string(filepath.Separator)):
			inv.Assets = append(inv.Assets, info.Name())
		}
		return nil
	})
	return inv
}
