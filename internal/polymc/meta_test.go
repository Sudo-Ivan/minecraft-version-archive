package polymc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"minecraft-version-archive/internal/store"
)

func TestIntermediaryVersion(t *testing.T) {
	data, err := IntermediaryVersion("1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	var file VersionFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if file.UID != UIDIntermediary || file.Version != "1.21.1" {
		t.Fatalf("unexpected version file: %+v", file)
	}
}

func TestFabricLoaderIndexFromPairs(t *testing.T) {
	root := t.TempDir()
	layout := store.New(root)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	pairs := `[{"loader":{"version":"0.16.9","stable":true},"intermediary":{"version":"1.21.1"}}]`
	path := layout.FabricMetaPath("v2/versions/loader/1.21.1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(pairs), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := FabricLoaderIndex(layout, "1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	var list VersionList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Versions) != 1 || list.Versions[0].Version != "0.16.9" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestLoaderIndexFromMCData(t *testing.T) {
	raw := `[{"loader":{"version":"0.19.3","stable":true}}]`
	data, err := LoaderIndexFromMCData([]byte(raw), "26.2")
	if err != nil {
		t.Fatal(err)
	}
	var list VersionList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Versions) != 1 || list.Versions[0].Version != "0.19.3" || !list.Versions[0].Recommended {
		t.Fatalf("unexpected list: %+v", list)
	}
}
