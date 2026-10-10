package temurin

import (
	"testing"
)

func TestParsePlatformsDefault(t *testing.T) {
	plats, err := ParsePlatforms(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plats) != len(DefaultPlatforms) {
		t.Fatalf("platforms = %d", len(plats))
	}
}

func TestParsePlatformsUnknown(t *testing.T) {
	_, err := ParsePlatforms([]string{"freebsd-mips"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPackageType(t *testing.T) {
	if got := PackageType("OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz"); got != "tar.gz" {
		t.Fatalf("got %q", got)
	}
	if got := PackageType("OpenJDK21U-jdk_x64_windows_hotspot_21.0.11_10.zip"); got != "zip" {
		t.Fatalf("got %q", got)
	}
}

func TestAppendMCLink(t *testing.T) {
	in := []byte(`{"id":"1.21.1","major_version":21,"component":"java-runtime-delta"}`)
	got := string(AppendMCLink(in, 21))
	want := `{"id":"1.21.1","major_version":21,"component":"java-runtime-delta","temurin":"/java/temurin/21"}`
	if got != want {
		t.Fatalf("got %q", got)
	}
}
