package archive

import (
	"context"
	"fmt"
	"os"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/fabric"
	"minecraft-version-archive/internal/store"
)

type FabricOptions struct {
	MCVersions    []string
	LoaderVersion string
	AllLoaders    bool
}

func (a *Archiver) RunFabric(ctx context.Context, opts FabricOptions) error {
	if err := a.layout.Ensure(); err != nil {
		return err
	}

	mcVersions, err := a.resolveFabricMCVersions(opts.MCVersions)
	if err != nil {
		return err
	}
	if len(mcVersions) == 0 {
		return fmt.Errorf("no minecraft versions to archive; archive vanilla first or pass version arguments")
	}

	if len(mcVersions) == 1 {
		a.log.Info("fabric archiving version", "version", mcVersions[0])
	} else {
		a.log.Info("fabric archiving versions", "versions", mcVersions)
	}

	loaderTotal := 0
	for _, mc := range mcVersions {
		n, err := a.countFabricLoaders(ctx, mc, opts)
		if err != nil {
			return err
		}
		loaderTotal += n
	}

	a.log.Info("fabric archive plan",
		"minecraft", len(mcVersions),
		"loaders", loaderTotal,
		"all_loaders", opts.AllLoaders,
		"workers", a.cfg.Workers,
	)

	prog := NewProgress(len(mcVersions), a.log)
	go prog.Run(ctx)

	if err := a.archiveFabricIndex(ctx, "v2/versions/loader", prog); err != nil {
		return err
	}
	if err := a.archiveFabricIndex(ctx, "v2/versions/intermediary", prog); err != nil {
		return err
	}

	for _, mc := range mcVersions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		prog.BeginVersion(mc)
		err := a.archiveFabricMC(ctx, mc, opts, prog)
		prog.EndVersion(mc, err == nil)
		if err != nil {
			prog.logStatus(true)
			return fmt.Errorf("%s: %w", mc, err)
		}
	}

	prog.logStatus(true)
	a.log.Info("fabric archive complete", "minecraft", len(mcVersions), "loaders", loaderTotal)
	return nil
}

func (a *Archiver) countFabricLoaders(ctx context.Context, mc string, opts FabricOptions) (int, error) {
	if opts.LoaderVersion != "" {
		return 1, nil
	}
	enc := fabric.EncodeGameVersion(mc)
	listData, err := a.fetch.Get(ctx, fabric.MetaURL("v2/versions/loader/"+enc))
	if err != nil {
		return 0, fmt.Errorf("loader list %s: %w", mc, err)
	}
	pairs, err := fabric.ParseLoaderPairs(listData)
	if err != nil {
		return 0, err
	}
	loaders := selectFabricLoaders(pairs, opts)
	return len(loaders), nil
}

func (a *Archiver) resolveFabricMCVersions(requested []string) ([]string, error) {
	if len(requested) > 0 {
		return requested, nil
	}
	entries, err := os.ReadDir(a.layout.VersionsDir())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(ent.Name(), ".json"))
	}
	return out, nil
}

func (a *Archiver) archiveFabricIndex(ctx context.Context, rel string, prog *Progress) error {
	return a.archiveFabricMeta(ctx, rel, prog)
}

func (a *Archiver) archiveFabricMC(ctx context.Context, mc string, opts FabricOptions, prog *Progress) error {
	enc := fabric.EncodeGameVersion(mc)
	listRel := "v2/versions/loader/" + enc
	listData, err := a.fetchMeta(ctx, listRel, prog)
	if err != nil {
		return fmt.Errorf("loader list: %w", err)
	}

	pairs, err := fabric.ParseLoaderPairs(listData)
	if err != nil {
		return fmt.Errorf("parse loader list: %w", err)
	}
	if len(pairs) == 0 {
		a.log.Info("fabric skip", "minecraft", mc, "reason", "no compatible loaders")
		return nil
	}

	loaders := selectFabricLoaders(pairs, opts)
	for _, loader := range loaders {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := a.archiveFabricLoader(ctx, mc, enc, loader, prog); err != nil {
			return err
		}
	}
	return nil
}

func selectFabricLoaders(pairs []fabric.LoaderPair, opts FabricOptions) []string {
	if opts.LoaderVersion != "" {
		return []string{opts.LoaderVersion}
	}
	if opts.AllLoaders {
		out := make([]string, 0, len(pairs))
		for _, p := range pairs {
			if p.Loader.Version != "" {
				out = append(out, p.Loader.Version)
			}
		}
		return out
	}
	for _, p := range pairs {
		if p.Loader.Stable && p.Loader.Version != "" {
			return []string{p.Loader.Version}
		}
	}
	if pairs[0].Loader.Version != "" {
		return []string{pairs[0].Loader.Version}
	}
	return nil
}

func (a *Archiver) archiveFabricLoader(ctx context.Context, mc, encMC, loader string, prog *Progress) error {
	encLoader := fabric.EncodeGameVersion(loader)
	detailRel := "v2/versions/loader/" + encMC + "/" + encLoader
	detailData, err := a.fetchMeta(ctx, detailRel, prog)
	if err != nil {
		return fmt.Errorf("loader %s: %w", loader, err)
	}

	for _, suffix := range []string{"profile/json", "server/json"} {
		rel := detailRel + "/" + suffix
		if err := a.archiveFabricMeta(ctx, rel, prog); err != nil {
			a.log.Debug("fabric profile skip", "minecraft", mc, "loader", loader, "path", suffix, "err", err)
		}
	}

	detail, err := fabric.ParseLoaderDetail(detailData)
	if err != nil {
		return err
	}
	for _, lib := range fabric.LibrariesFromDetail(detail) {
		if err := a.archiveFabricLibrary(ctx, lib, prog); err != nil {
			return err
		}
	}
	if err := a.archiveFabricLoaderJSON(ctx, loader, prog); err != nil {
		a.log.Debug("fabric loader json skip", "loader", loader, "err", err)
	}
	return nil
}

func (a *Archiver) fetchMeta(ctx context.Context, rel string, prog *Progress) ([]byte, error) {
	prog.AddTasks(1)
	data, err := a.fetch.Get(ctx, fabric.MetaURL(rel))
	if err != nil {
		prog.TaskDone(0)
		return nil, err
	}
	if err := storeFabricMeta(a.layout, rel, data); err != nil {
		prog.TaskDone(0)
		return nil, err
	}
	prog.TaskDone(int64(len(data)))
	return data, nil
}

func (a *Archiver) archiveFabricMeta(ctx context.Context, rel string, prog *Progress) error {
	_, err := a.fetchMeta(ctx, rel, prog)
	return err
}

func (a *Archiver) archiveFabricLibrary(ctx context.Context, lib fabric.Library, prog *Progress) error {
	if lib.Name == "" {
		return nil
	}
	base := lib.URL
	if base == "" {
		base = constants.FabricMavenBase + "/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	rel := fabric.MavenRelPath(lib.Name)
	if rel == "" {
		return nil
	}
	dest := a.layout.FabricMavenPath(rel)
	prog.AddTasks(1)
	if a.layout.Exists(dest) {
		prog.TaskDone(fileSize(dest))
		return nil
	}
	url := fabric.MavenDownloadURL(base, lib.Name)
	if url == "" {
		prog.TaskDone(0)
		return nil
	}
	if err := a.fetch.Download(ctx, url, dest); err != nil {
		prog.TaskDone(0)
		return err
	}
	prog.TaskDone(fileSize(dest))
	return nil
}

func (a *Archiver) archiveFabricLoaderJSON(ctx context.Context, loaderVersion string, prog *Progress) error {
	coord := "net.fabricmc:fabric-loader:" + loaderVersion + ":json"
	rel := fabric.MavenRelPath(coord)
	if rel == "" {
		return fmt.Errorf("invalid loader coord")
	}
	dest := a.layout.FabricMavenPath(rel)
	prog.AddTasks(1)
	if a.layout.Exists(dest) {
		prog.TaskDone(fileSize(dest))
		return nil
	}
	url := fabric.MavenDownloadURL("https://maven.fabricmc.net/", coord)
	if err := a.fetch.Download(ctx, url, dest); err != nil {
		prog.TaskDone(0)
		return err
	}
	prog.TaskDone(fileSize(dest))
	return nil
}

func storeFabricMeta(layout *store.Layout, rel string, data []byte) error {
	path := layout.FabricMetaPath(fabric.MetaStorageRel(rel))
	return store.WriteBytes(layout, path, data)
}
