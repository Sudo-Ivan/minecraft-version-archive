package mojang

import (
	"strings"
	"testing"
)

func TestVersionChannel(t *testing.T) {
	cases := map[string]string{
		"26.1.2|release":            "release",
		"26w14a|snapshot":           "snapshot",
		"26.1-pre-3|snapshot":       "prerelease",
		"26.1-rc-2|snapshot":        "rc",
		"26.1-snapshot-11|snapshot": "snapshot",
		"a1.0.0|old_alpha":          "alpha",
		"b1.0.0|old_beta":           "beta",
	}
	for key, want := range cases {
		id, typ, _ := stringsCut(key, "|")
		if got := VersionChannel(id, typ); got != want {
			t.Fatalf("VersionChannel(%q, %q) = %q, want %q", id, typ, got, want)
		}
	}
}

func stringsCut(s, sep string) (string, string, bool) {
	before, after, ok := strings.Cut(s, sep)
	return before, after, ok
}

func TestFilterVersionIDsReleasesOnly(t *testing.T) {
	versions := []ManifestVersion{
		{ID: "26.1.2", Type: "release"},
		{ID: "26w14a", Type: "snapshot"},
		{ID: "26.1-pre-1", Type: "snapshot"},
		{ID: "a1.0.0", Type: "old_alpha"},
	}
	ids := FilterVersionIDs(versions, func(ch string) bool { return ch == "release" })
	if len(ids) != 1 || ids[0] != "26.1.2" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestFilterVersionIDsWithSnapshots(t *testing.T) {
	versions := []ManifestVersion{
		{ID: "26.1.2", Type: "release"},
		{ID: "26w14a", Type: "snapshot"},
		{ID: "26.1-pre-1", Type: "snapshot"},
		{ID: "26.1-rc-1", Type: "snapshot"},
	}
	ids := FilterVersionIDs(versions, func(ch string) bool {
		return ch == "release" || ch == "snapshot" || ch == "prerelease"
	})
	if len(ids) != 3 {
		t.Fatalf("ids = %v", ids)
	}
}
