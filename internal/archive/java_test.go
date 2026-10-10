package archive

import (
	"os"
	"testing"

	"minecraft-version-archive/internal/config"
	"minecraft-version-archive/internal/store"
)

func TestResolveJavaMajorsFromVersions(t *testing.T) {
	dir := t.TempDir()
	layout := store.New(dir)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	for id, raw := range map[string]string{
		"1.20.1": `{"id":"1.20.1","javaVersion":{"majorVersion":17,"component":"java-runtime-gamma"}}`,
		"1.21.1": `{"id":"1.21.1","javaVersion":{"majorVersion":21,"component":"java-runtime-delta"}}`,
	} {
		if err := os.WriteFile(layout.VersionJSON(id), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &Archiver{cfg: &config.Config{DataDir: dir}, layout: layout}
	majors, err := a.resolveJavaMajors(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(majors) != 2 || majors[0] != 17 || majors[1] != 21 {
		t.Fatalf("majors = %v", majors)
	}
}
