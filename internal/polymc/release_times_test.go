package polymc

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLoaderIndexFromMCDataReleaseTimes(t *testing.T) {
	SetReleaseTimesFetcher(func(ctx context.Context, url string) ([]byte, error) {
		return []byte(`{
			"formatVersion": 1,
			"uid": "net.fabricmc.fabric-loader",
			"name": "Fabric Loader",
			"versions": [
				{"version": "0.19.3", "releaseTime": "2026-06-01T17:23:10+00:00", "type": "release"}
			]
		}`), nil
	})

	raw := `[{"loader":{"version":"0.19.3","stable":true}}]`
	data, err := LoaderIndexFromMCDataWithTimes([]byte(raw), "26.2", LoaderReleaseTimes(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	var list VersionList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Versions) != 1 {
		t.Fatalf("versions = %d", len(list.Versions))
	}
	if list.Versions[0].ReleaseTime != "2026-06-01T17:23:10+00:00" {
		t.Fatalf("releaseTime = %q", list.Versions[0].ReleaseTime)
	}
	if len(list.Versions[0].Requires) != 1 {
		t.Fatalf("requires = %+v", list.Versions[0].Requires)
	}
	if list.Versions[0].Requires[0].UID != UIDIntermediary {
		t.Fatalf("requires = %+v", list.Versions[0].Requires)
	}
}
