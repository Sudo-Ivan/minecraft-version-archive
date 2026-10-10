package fabric

import "testing"

func TestMetaStorageRel(t *testing.T) {
	cases := []struct {
		api  string
		want string
	}{
		{"v2/versions/loader", "v2/versions/loader.json"},
		{"v2/versions/intermediary", "v2/versions/intermediary.json"},
		{"v2/versions/loader/26.1.2", "v2/versions/loader/_mc/26.1.2.json"},
		{"v2/versions/loader/26.1.2/0.16.9", "v2/versions/loader/_mc/26.1.2/_loader/0.16.9.json"},
		{"v2/versions/loader/26.1.2/0.16.9/profile/json", "v2/versions/loader/_mc/26.1.2/_loader/0.16.9/profile.json"},
		{"v2/versions/loader/1.14 Pre-Release 5", "v2/versions/loader/_mc/1.14%20Pre-Release%205.json"},
		{"v2/versions/loader/1.14%20Pre-Release%205/0.16.9", "v2/versions/loader/_mc/1.14%20Pre-Release%205/_loader/0.16.9.json"},
	}
	for _, tc := range cases {
		got := MetaStorageRel(tc.api)
		if got != tc.want {
			t.Fatalf("%q -> %q want %q", tc.api, got, tc.want)
		}
	}
}

func TestMavenRelPath(t *testing.T) {
	got := MavenRelPath("net.fabricmc:fabric-loader:0.16.9")
	want := "net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMavenRelPathClassifier(t *testing.T) {
	got := MavenRelPath("net.fabricmc:fabric-loader:0.16.9:json")
	want := "net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMavenDownloadURL(t *testing.T) {
	got := MavenDownloadURL("https://maven.fabricmc.net/", "org.ow2.asm:asm:9.6")
	want := "https://maven.fabricmc.net/org/ow2/asm/asm/9.6/asm-9.6.jar"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRewriteMeta(t *testing.T) {
	in := []byte(`{"url":"https://maven.fabricmc.net/net/fabricmc/"}`)
	got := string(RewriteMeta(in, "https://mirror.example"))
	if got != `{"url":"https://mirror.example/maven/fabricmc/net/fabricmc/"}` {
		t.Fatalf("got %s", got)
	}
}

func TestEncodeGameVersion(t *testing.T) {
	got := EncodeGameVersion("1.14 Pre-Release 5")
	if got != "1.14%20Pre-Release%205" {
		t.Fatalf("got %q", got)
	}
	if EncodeGameVersion("1.14%20Pre-Release%205") != "1.14%20Pre-Release%205" {
		t.Fatal("double encode")
	}
}

func TestMetaURLEncodesVersions(t *testing.T) {
	got := MetaURL("v2/versions/loader/1.14 Pre-Release 5")
	want := "https://meta.fabricmc.net/v2/versions/loader/1.14%20Pre-Release%205"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
