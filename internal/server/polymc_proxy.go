package server

import (
	"context"
	"strings"

	"minecraft-version-archive/internal/fabric"
	"minecraft-version-archive/internal/polymc"
)

func (s *Server) proxyPolyMCMeta(ctx context.Context, relPath, mcVersion, publicURL string) ([]byte, bool, error) {
	times := polymc.LoaderReleaseTimes(ctx)
	relPath = strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "/")
	switch relPath {
	case polymc.UIDFabricLoader + "/index.json":
		if mcVersion != "" {
			apiRel := "v2/versions/loader/" + fabric.EncodeGameVersion(mcVersion)
			raw, err := s.proxy.fabricMeta(ctx, apiRel)
			if err != nil {
				return nil, false, err
			}
			data, err := polymc.LoaderIndexFromMCDataWithTimes(raw, mcVersion, times)
			return data, err == nil, err
		}
		raw, err := s.proxy.fabricMeta(ctx, "v2/versions/loader")
		if err != nil {
			return nil, false, err
		}
		data, err := polymc.LoaderIndexFromGlobalDataWithTimes(raw, times)
		return data, err == nil, err
	}

	if strings.HasPrefix(relPath, polymc.UIDFabricLoader+"/") && strings.HasSuffix(relPath, ".json") {
		loaderVersion := strings.TrimSuffix(strings.TrimPrefix(relPath, polymc.UIDFabricLoader+"/"), ".json")
		if loaderVersion == "" {
			return nil, false, nil
		}
		candidates := []string{mcVersion}
		if mcVersion == "" {
			candidates = s.fabricMCCandidatesFromManifest(ctx)
		}
		var lastErr error
		for _, mc := range candidates {
			if mc == "" {
				continue
			}
			apiRel := "v2/versions/loader/" + fabric.EncodeGameVersion(mc) + "/" + loaderVersion
			raw, err := s.proxy.fabricMeta(ctx, apiRel)
			if err != nil {
				lastErr = err
				continue
			}
			data, err := polymc.LoaderVersionFromDetailData(raw, loaderVersion, mc, publicURL, times)
			if err != nil {
				lastErr = err
				continue
			}
			return data, true, nil
		}
		if lastErr != nil {
			return nil, false, lastErr
		}
		return nil, false, nil
	}

	return nil, false, nil
}

func (s *Server) fabricMCCandidatesFromManifest(ctx context.Context) []string {
	manifest, err := s.proxy.manifest(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(manifest.Versions))
	for _, v := range manifest.Versions {
		if v.ID != "" {
			out = append(out, v.ID)
		}
	}
	return out
}
