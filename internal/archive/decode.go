package archive

import (
	"encoding/json"

	"minecraft-version-archive/internal/mojang"
	"minecraft-version-archive/internal/store"
)

func decodeVersion(data []byte, v *mojang.Version) error {
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	v.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func decodeAssetIndexFile(layout *store.Layout, path string) (*mojang.AssetIndex, error) {
	data, err := layout.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeAssetIndex(data)
}

func decodeAssetIndex(data []byte) (*mojang.AssetIndex, error) {
	var idx mojang.AssetIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}
