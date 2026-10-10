package archive

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/mojang"
	"minecraft-version-archive/internal/store"
	"minecraft-version-archive/internal/temurin"
)

type JavaOptions struct {
	Majors    []int
	Platforms []string
}

func (a *Archiver) RunJava(ctx context.Context, opts JavaOptions) error {
	if err := a.layout.Ensure(); err != nil {
		return err
	}

	platforms, err := temurin.ParsePlatforms(opts.Platforms)
	if err != nil {
		return err
	}

	majors, err := a.resolveJavaMajors(opts.Majors)
	if err != nil {
		return err
	}
	if len(majors) == 0 {
		return fmt.Errorf("no java majors to archive; archive minecraft versions first or pass major versions")
	}

	a.log.Info("java archive plan",
		"majors", majors,
		"platforms", temurin.PlatformIDs(platforms),
		"workers", a.cfg.Workers,
	)

	for _, major := range majors {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := a.archiveJavaMajor(ctx, major, platforms); err != nil {
			return fmt.Errorf("java %d: %w", major, err)
		}
	}

	if err := temurin.WriteGlobalIndex(a.layout, majors); err != nil {
		return err
	}
	a.log.Info("java archive complete", "majors", len(majors), "platforms", len(platforms))
	return nil
}

func (a *Archiver) resolveJavaMajors(requested []int) ([]int, error) {
	if len(requested) > 0 {
		out := append([]int(nil), requested...)
		sort.Ints(out)
		return dedupeSortedInts(out), nil
	}
	entries, err := os.ReadDir(a.layout.VersionsDir())
	if err != nil {
		return nil, err
	}
	seen := make(map[int]struct{})
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := a.layout.ReadFile(a.layout.VersionJSON(strings.TrimSuffix(ent.Name(), ".json")))
		if err != nil {
			continue
		}
		info, ok := mojang.ExtractJavaVersion(data)
		if !ok || info.Major < 1 {
			continue
		}
		seen[info.Major] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	out := make([]int, 0, len(seen))
	for major := range seen {
		out = append(out, major)
	}
	sort.Ints(out)
	return out, nil
}

func dedupeSortedInts(in []int) []int {
	if len(in) == 0 {
		return in
	}
	out := in[:1]
	for i := 1; i < len(in); i++ {
		if in[i] != out[len(out)-1] {
			out = append(out, in[i])
		}
	}
	return out
}

func (a *Archiver) archiveJavaMajor(ctx context.Context, major int, platforms []temurin.Platform) error {
	var rel *temurin.Release
	entries := make([]temurin.PlatformEntry, 0, len(platforms))
	for _, plat := range platforms {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		release, bin, err := temurin.FetchRelease(ctx, a.fetch, major, plat)
		if temurin.IsUnavailable(err) {
			a.log.Warn("skipping unavailable java platform", "major", major, "platform", plat.ID, "err", err)
			continue
		}
		if err != nil {
			return err
		}
		if rel == nil {
			rel = release
		}
		dest := temurin.PlatformFilePath(a.layout, major, plat.ID, bin.Package.Name)
		spec := integrity.FromSHA256(bin.Package.Checksum, bin.Package.Size)
		if err := a.ensureJavaBinary(ctx, bin.Package.Link, dest, spec); err != nil {
			return fmt.Errorf("%s: %w", plat.ID, err)
		}
		entries = append(entries, temurin.EntryFromBinary(a.cfg.PublicURL, major, plat, release, bin))
		a.log.Info("archived java", "major", major, "platform", plat.ID, "file", bin.Package.Name)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no temurin platforms available for java %d", major)
	}
	man := temurin.BuildMajorManifest(major, rel, entries)
	return store.WriteJSON(a.layout, temurin.MajorManifestPath(a.layout, major), man)
}

func (a *Archiver) ensureJavaBinary(ctx context.Context, url, dest string, spec integrity.Spec) error {
	dir, err := a.layout.Dir()
	if err != nil {
		return err
	}
	if err := integrity.VerifyFile(dir, dest, spec); err == nil {
		return nil
	}
	_ = os.Remove(dest)
	return a.fetch.DownloadVerified(ctx, url, dest, spec)
}
