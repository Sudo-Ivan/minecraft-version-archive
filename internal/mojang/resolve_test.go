package mojang

import (
	"strings"
	"testing"
)

func TestResolveVersionIDExact(t *testing.T) {
	versions := []ManifestVersion{{ID: "1.20.1"}, {ID: "1.5"}}
	got, err := ResolveVersionID("1.20.1", versions)
	if err != nil || got != "1.20.1" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveVersionIDAlias(t *testing.T) {
	versions := []ManifestVersion{{ID: "1.5"}, {ID: "1.20.1"}}
	got, err := ResolveVersionID("1.5.0", versions)
	if err != nil || got != "1.5" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveVersionIDSuggestions(t *testing.T) {
	versions := []ManifestVersion{{ID: "1.20.1"}, {ID: "1.20.2"}}
	_, err := ResolveVersionID("1.20", versions)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "1.20.1") || !strings.Contains(msg, "1.20.2") {
		t.Fatalf("err = %v", err)
	}
}
