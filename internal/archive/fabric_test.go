package archive

import (
	"testing"

	"minecraft-version-archive/internal/fabric"
)

func TestSelectFabricLoadersStable(t *testing.T) {
	pairs := []fabric.LoaderPair{
		{Loader: struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
			Maven   string `json:"maven"`
		}{Version: "0.16.10", Stable: false}},
		{Loader: struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
			Maven   string `json:"maven"`
		}{Version: "0.16.9", Stable: true}},
	}
	got := selectFabricLoaders(pairs, FabricOptions{})
	if len(got) != 1 || got[0] != "0.16.9" {
		t.Fatalf("got %v", got)
	}
}

func TestSelectFabricLoadersExplicit(t *testing.T) {
	pairs := []fabric.LoaderPair{}
	got := selectFabricLoaders(pairs, FabricOptions{LoaderVersion: "0.15.0"})
	if len(got) != 1 || got[0] != "0.15.0" {
		t.Fatalf("got %v", got)
	}
}
