package archive

import (
	"context"
	"fmt"

	"minecraft-version-archive/internal/mojang"
)

func (a *Archiver) loadVersionJSON(ctx context.Context, id, mojangURL, versionPath string) (*mojang.Version, []byte, error) {
	v, data, err := a.mojang.FetchVersion(ctx, mojangURL)
	if err == nil {
		return v, data, nil
	}

	if a.peers != nil {
		peer, perr := a.peers.FindVersion(ctx, id)
		if perr == nil {
			if err := a.peers.FetchVersionJSON(ctx, peer, id, versionPath); err == nil {
				data, err := a.layout.ReadFile(versionPath)
				if err != nil {
					return nil, nil, err
				}
				var v mojang.Version
				if err := decodeVersion(data, &v); err != nil {
					return nil, nil, err
				}
				return &v, data, nil
			}
		}
	}

	if a.layout.Exists(versionPath) {
		data, rerr := a.layout.ReadFile(versionPath)
		if rerr != nil {
			return nil, nil, fmt.Errorf("fetch version json: %w", err)
		}
		var v mojang.Version
		if err := decodeVersion(data, &v); err != nil {
			return nil, nil, err
		}
		return &v, data, nil
	}

	return nil, nil, fmt.Errorf("fetch version json: %w", err)
}
