package mojang

import (
	"testing"
)

const sampleVersionJSON = `{
  "id": "1.20.1",
  "javaVersion": {
    "component": "java-runtime-gamma",
    "majorVersion": 17
  }
}`

func TestExtractJavaVersion(t *testing.T) {
	info, ok := ExtractJavaVersion([]byte(sampleVersionJSON))
	if !ok {
		t.Fatal("expected ok")
	}
	if info.Major != 17 || info.Component != "java-runtime-gamma" {
		t.Fatalf("info = %+v", info)
	}
}

func TestExtractJavaVersionCompact(t *testing.T) {
	raw := []byte(`{"javaVersion":{"component":"jre-legacy","majorVersion":8}}`)
	info, ok := ExtractJavaVersion(raw)
	if !ok || info.Major != 8 || info.Component != "jre-legacy" {
		t.Fatalf("info = %+v ok=%v", info, ok)
	}
}

func TestExtractJavaVersionMissing(t *testing.T) {
	_, ok := ExtractJavaVersion([]byte(`{"id":"1.0"}`))
	if ok {
		t.Fatal("expected false")
	}
}

func TestAppendJavaResponse(t *testing.T) {
	got := string(AppendJavaResponse(nil, "1.20.1", JavaInfo{Major: 17, Component: "java-runtime-gamma"}))
	want := `{"id":"1.20.1","major_version":17,"component":"java-runtime-gamma"}`
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestVersionIDCandidates(t *testing.T) {
	got := VersionIDCandidates("1.5.0")
	if len(got) < 2 || got[1] != "1.5" {
		t.Fatalf("got %v", got)
	}
}

func BenchmarkExtractJavaVersion(b *testing.B) {
	data := []byte(sampleVersionJSON)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		ExtractJavaVersion(data)
	}
}

func BenchmarkAppendJavaResponse(b *testing.B) {
	info := JavaInfo{Major: 17, Component: "java-runtime-gamma"}
	buf := make([]byte, 0, 96)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf = AppendJavaResponse(buf[:0], "1.20.1", info)
	}
}

func BenchmarkExtractJavaVersionNoAllocComponent(b *testing.B) {
	data := []byte(sampleVersionJSON)
	var sink string
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		info, ok := ExtractJavaVersion(data)
		if !ok {
			b.Fatal()
		}
		sink = info.Component
	}
	_ = sink
}
