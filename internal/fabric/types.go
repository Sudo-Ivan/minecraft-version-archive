package fabric

import (
	"encoding/json"
	"strings"

	"minecraft-version-archive/internal/constants"
)

type VersionEntry struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Maven   string `json:"maven"`
}

type LoaderPair struct {
	Loader struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
		Maven   string `json:"maven"`
	} `json:"loader"`
	Intermediary struct {
		Version string `json:"version"`
		Maven   string `json:"maven"`
	} `json:"intermediary"`
}

type Library struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type LauncherMeta struct {
	Libraries struct {
		Client []Library `json:"client"`
		Common []Library `json:"common"`
		Server []Library `json:"server"`
	} `json:"libraries"`
}

type LoaderDetail struct {
	Loader       VersionEntry `json:"loader"`
	Intermediary VersionEntry `json:"intermediary"`
	LauncherMeta LauncherMeta `json:"launcherMeta"`
}

func ParseLoaderDetail(data []byte) (LoaderDetail, error) {
	var detail LoaderDetail
	err := json.Unmarshal(data, &detail)
	return detail, err
}

func ParseLoaderPairs(data []byte) ([]LoaderPair, error) {
	var pairs []LoaderPair
	err := json.Unmarshal(data, &pairs)
	return pairs, err
}

func LibrariesFromDetail(detail LoaderDetail) []Library {
	var out []Library
	out = append(out, detail.LauncherMeta.Libraries.Common...)
	out = append(out, detail.LauncherMeta.Libraries.Client...)
	out = append(out, detail.LauncherMeta.Libraries.Server...)
	if detail.Loader.Maven != "" {
		out = append(out, Library{Name: mavenCoord(detail.Loader.Maven), URL: constants.FabricMavenBase})
	}
	if detail.Intermediary.Maven != "" {
		out = append(out, Library{Name: mavenCoord(detail.Intermediary.Maven), URL: constants.FabricMavenBase})
	}
	return out
}

func mavenCoord(maven string) string {
	if _, after, ok := strings.Cut(maven, ":"); ok {
		return after
	}
	return maven
}
