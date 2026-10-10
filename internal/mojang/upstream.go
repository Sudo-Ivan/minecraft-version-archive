package mojang

import (
	"encoding/json"
	"fmt"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/integrity"
)

func LibraryUpstreamURL(path string) string {
	return constants.MojangLibBase + "/" + strings.TrimPrefix(path, "/")
}

func AssetObjectUpstreamURL(hash string) string {
	if len(hash) < 2 {
		return ""
	}
	return constants.MojangAssetBase + "/" + hash[:2] + "/" + hash
}

func DownloadURLFromVersionJSON(raw []byte, kind string) (string, error) {
	url, _, err := DownloadInfoFromVersionJSON(raw, kind)
	return url, err
}

func DownloadInfoFromVersionJSON(raw []byte, kind string) (url string, spec integrity.Spec, err error) {
	var doc struct {
		Downloads map[string]struct {
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
		} `json:"downloads"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", integrity.Spec{}, err
	}
	item, ok := doc.Downloads[kind]
	if !ok || item.URL == "" {
		return "", integrity.Spec{}, fmt.Errorf("no %s download in version json", kind)
	}
	return item.URL, integrity.Spec{SHA1: item.SHA1, Size: item.Size}, nil
}

func AssetIndexUpstreamURL(raw []byte) (string, error) {
	var doc struct {
		AssetIndex struct {
			URL string `json:"url"`
		} `json:"assetIndex"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	if doc.AssetIndex.URL == "" {
		return "", fmt.Errorf("no asset index url")
	}
	return doc.AssetIndex.URL, nil
}

func ManifestVersionURL(manifest *Manifest, id string) (string, error) {
	resolved, err := ResolveVersionID(id, manifest.Versions)
	if err != nil {
		return "", err
	}
	for _, v := range manifest.Versions {
		if v.ID == resolved {
			if v.URL == "" {
				return "", fmt.Errorf("version %q has no upstream url", resolved)
			}
			return v.URL, nil
		}
	}
	return "", fmt.Errorf("unknown version %q", id)
}
