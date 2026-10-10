package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"minecraft-version-archive/internal/fabric"
	"minecraft-version-archive/internal/mojang"
)

func CollectExportPaths(layout *Layout, versionIDs []string) (map[string]struct{}, error) {
	paths := make(map[string]struct{})
	for _, id := range versionIDs {
		if err := addVersionExportPaths(layout, id, paths); err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		done := filepath.Join(layout.CoordinatorDoneDir(), safeVersionName(id))
		if layout.Exists(done) {
			addRelPath(layout, done, paths)
		}
	}
	if layout.Exists(filepath.Join(layout.InventoryDir(), "sha1_index.json")) {
		addRelPath(layout, filepath.Join(layout.InventoryDir(), "sha1_index.json"), paths)
	}
	addFabricGlobalExportPaths(layout, paths)
	for _, id := range versionIDs {
		addFabricExportPaths(layout, id, paths)
	}
	return paths, nil
}

func addVersionExportPaths(layout *Layout, id string, paths map[string]struct{}) error {
	versionPath := layout.VersionJSON(id)
	if !layout.Exists(versionPath) {
		return fmt.Errorf("version json not found")
	}
	addRelPath(layout, versionPath, paths)

	for _, jar := range []string{layout.ClientJar(id), layout.ServerJar(id)} {
		if layout.Exists(jar) {
			addRelPath(layout, jar, paths)
		}
	}

	data, err := layout.ReadFile(versionPath)
	if err != nil {
		return err
	}
	var version mojang.Version
	if err := json.Unmarshal(data, &version); err != nil {
		return err
	}

	for _, lib := range version.Libraries {
		artifactPath := mojang.LibraryArtifactPath(lib)
		if artifactPath == "" {
			continue
		}
		libPath := layout.LibraryPath(artifactPath)
		if layout.Exists(libPath) {
			addRelPath(layout, libPath, paths)
		}
	}

	if version.AssetIndex.ID == "" {
		return nil
	}
	indexPath := layout.AssetIndexPath(version.AssetIndex.ID)
	if !layout.Exists(indexPath) {
		return nil
	}
	addRelPath(layout, indexPath, paths)

	indexData, err := layout.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var idx mojang.AssetIndex
	if err := json.Unmarshal(indexData, &idx); err != nil {
		return err
	}
	for _, obj := range idx.Objects {
		objPath := layout.AssetObjectPath(obj.Hash)
		if layout.Exists(objPath) {
			addRelPath(layout, objPath, paths)
		}
	}
	return nil
}

func addFabricGlobalExportPaths(layout *Layout, paths map[string]struct{}) {
	for _, rel := range []string{"v2/versions/loader", "v2/versions/intermediary"} {
		path := layout.FabricMetaPath(rel)
		if layout.Exists(path) {
			addRelPath(layout, path, paths)
		}
	}
}

func addFabricExportPaths(layout *Layout, id string, paths map[string]struct{}) {
	enc := fabric.EncodeGameVersion(id)
	listRel := "v2/versions/loader/" + enc
	listPath := layout.FabricMetaPath(listRel)
	if !layout.Exists(listPath) {
		return
	}
	addRelPath(layout, listPath, paths)
	data, err := layout.ReadFile(listPath)
	if err != nil {
		return
	}
	pairs, err := fabric.ParseLoaderPairs(data)
	if err != nil {
		return
	}
	for _, pair := range pairs {
		if pair.Loader.Version == "" {
			continue
		}
		detailRel := listRel + "/" + fabric.EncodeGameVersion(pair.Loader.Version)
		detailPath := layout.FabricMetaPath(detailRel)
		if !layout.Exists(detailPath) {
			continue
		}
		addRelPath(layout, detailPath, paths)
		for _, suffix := range []string{"profile/json", "server/json"} {
			profilePath := layout.FabricMetaPath(detailRel + "/" + suffix)
			if layout.Exists(profilePath) {
				addRelPath(layout, profilePath, paths)
			}
		}
		detailData, err := layout.ReadFile(detailPath)
		if err != nil {
			continue
		}
		detail, err := fabric.ParseLoaderDetail(detailData)
		if err != nil {
			continue
		}
		for _, lib := range fabric.LibrariesFromDetail(detail) {
			rel := fabric.MavenRelPath(lib.Name)
			if rel == "" {
				continue
			}
			mavenPath := layout.FabricMavenPath(rel)
			if layout.Exists(mavenPath) {
				addRelPath(layout, mavenPath, paths)
			}
		}
	}
}

func FilterManifestVersions(layout *Layout, manifestPath string, versionIDs []string) ([]byte, error) {
	data, err := layout.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var manifest mojang.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(versionIDs))
	for _, id := range versionIDs {
		set[id] = struct{}{}
	}
	filtered := make([]mojang.ManifestVersion, 0, len(versionIDs))
	for _, v := range manifest.Versions {
		if _, ok := set[v.ID]; ok {
			filtered = append(filtered, v)
		}
	}
	manifest.Versions = filtered
	return json.MarshalIndent(manifest, "", "  ")
}

func addRelPath(layout *Layout, abs string, paths map[string]struct{}) {
	rel, err := filepath.Rel(layout.Root, abs)
	if err != nil {
		return
	}
	paths[filepath.ToSlash(rel)] = struct{}{}
}

func safeVersionName(version string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "").Replace(version)
}
