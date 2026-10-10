package polymc

import (
	"encoding/json"
	"os"
	"testing"

	"minecraft-version-archive/internal/store"
)

const testVersionJSON = `{
	"id": "1.21.1",
	"type": "release",
	"releaseTime": "2024-08-08T12:24:45+00:00",
	"mainClass": "net.minecraft.client.main.Main",
	"minimumLauncherVersion": 21,
	"javaVersion": {"component": "java-runtime-delta", "majorVersion": 21},
	"downloads": {
		"client": {"sha1": "abc123", "size": 26836906, "url": "https://piston-data.mojang.com/v1/objects/abc123/client.jar"},
		"server": {"sha1": "def456", "size": 1, "url": "https://piston-data.mojang.com/v1/objects/def456/server.jar"}
	},
	"assetIndex": {"id": "17", "sha1": "0055", "size": 449557, "totalSize": 825109050, "url": "https://piston-meta.mojang.com/v1/packages/0055/17.json"},
	"arguments": {
		"game": ["--username", "${auth_player_name}", {"rules": [{"action": "allow", "features": {"is_quick_play_singleplayer": true}}], "value": ["--quickPlaySingleplayer", "${quickPlaySingleplayer}"]}],
		"jvm": [{"rules": [{"action": "allow", "os": {"name": "osx"}}], "value": ["-XstartOnFirstThread"]}]
	},
	"libraries": [
		{"name": "com.mojang:brigadier:1.3.10", "downloads": {"artifact": {"path": "com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar", "sha1": "d15b", "size": 80082, "url": "https://libraries.minecraft.net/com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar"}}},
		{"name": "org.lwjgl:lwjgl:3.3.3", "downloads": {"artifact": {"sha1": "a1", "size": 1, "url": "https://libraries.minecraft.net/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3.jar"}}},
		{"name": "org.lwjgl:lwjgl:3.3.3:natives-linux", "rules": [{"action": "allow", "os": {"name": "linux"}}], "downloads": {"artifact": {"sha1": "b2", "size": 2, "url": "https://libraries.minecraft.net/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-linux.jar"}}}
	],
	"logging": {"client": {"file": {"id": "client-1.12.xml", "sha1": "bd65", "size": 888, "url": "https://piston-data.mojang.com/v1/objects/bd65/client-1.12.xml"}, "type": "log4j2-xml"}}
}`

func writeTestVersion(t *testing.T) *store.Layout {
	t.Helper()
	root := t.TempDir()
	layout := store.New(root)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.VersionJSON("1.21.1"), []byte(testVersionJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return layout
}

func TestMinecraftDetail(t *testing.T) {
	layout := writeTestVersion(t)
	data, err := MinecraftDetail(layout, "1.21.1", "http://mirror")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["uid"] != UIDMinecraft || doc["version"] != "1.21.1" || doc["formatVersion"] != float64(1) {
		t.Fatalf("bad envelope: %v", doc)
	}
	if doc["minecraftArguments"] != "--username ${auth_player_name}" {
		t.Fatalf("bad minecraftArguments: %v", doc["minecraftArguments"])
	}
	traits, _ := doc["+traits"].([]any)
	want := map[string]bool{"FirstThreadOnMacOS": true, "feature:is_quick_play_singleplayer": true, "XR:Initial": true}
	for _, tr := range traits {
		delete(want, tr.(string))
	}
	if len(want) != 0 {
		t.Fatalf("missing traits: %v", want)
	}
	reqs, _ := doc["requires"].([]any)
	if len(reqs) != 1 || reqs[0].(map[string]any)["uid"] != UIDLWJGL3 || reqs[0].(map[string]any)["suggests"] != "3.3.3" {
		t.Fatalf("bad requires: %v", doc["requires"])
	}
	libs, _ := doc["libraries"].([]any)
	if len(libs) != 1 {
		t.Fatalf("expected 1 non-lwjgl library, got %d", len(libs))
	}
	art := libs[0].(map[string]any)["downloads"].(map[string]any)["artifact"].(map[string]any)
	if art["url"] != "http://mirror/libraries/com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar" {
		t.Fatalf("library url not rewritten: %v", art["url"])
	}
	mainJar := doc["mainJar"].(map[string]any)
	if mainJar["name"] != "com.mojang:minecraft:1.21.1:client" {
		t.Fatalf("bad mainJar name: %v", mainJar["name"])
	}
	mjArt := mainJar["downloads"].(map[string]any)["artifact"].(map[string]any)
	if mjArt["url"] != "http://mirror/jars/client/1.21.1.jar" || mjArt["sha1"] != "abc123" {
		t.Fatalf("bad mainJar artifact: %v", mjArt)
	}
	ai := doc["assetIndex"].(map[string]any)
	if ai["url"] != "http://mirror/assets/indexes/17.json" || ai["id"] != "17" {
		t.Fatalf("bad assetIndex: %v", ai)
	}
	if majors, _ := doc["compatibleJavaMajors"].([]any); len(majors) != 1 || majors[0] != float64(21) {
		t.Fatalf("bad compatibleJavaMajors: %v", doc["compatibleJavaMajors"])
	}
	if doc["compatibleJavaName"] != "java-runtime-delta" {
		t.Fatalf("bad compatibleJavaName: %v", doc["compatibleJavaName"])
	}
}

func TestMinecraftIndex(t *testing.T) {
	layout := writeTestVersion(t)
	data, err := MinecraftIndex(layout, "http://mirror")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		UID      string         `json:"uid"`
		Versions []VersionEntry `json:"versions"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if list.UID != UIDMinecraft || len(list.Versions) != 1 {
		t.Fatalf("bad index: %s", data)
	}
	v := list.Versions[0]
	if v.Version != "1.21.1" || v.Type != "release" || v.SHA256 == "" {
		t.Fatalf("bad entry: %+v", v)
	}
	if len(v.Requires) != 1 || v.Requires[0].UID != UIDLWJGL3 || v.Requires[0].Suggests != "3.3.3" {
		t.Fatalf("bad requires: %+v", v.Requires)
	}
}

func TestLWJGLIndexAndVersion(t *testing.T) {
	layout := writeTestVersion(t)
	data, err := LWJGLIndex(layout, "http://mirror", UIDLWJGL3)
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		UID      string         `json:"uid"`
		Versions []VersionEntry `json:"versions"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if list.UID != UIDLWJGL3 || len(list.Versions) != 1 || list.Versions[0].Version != "3.3.3" {
		t.Fatalf("bad lwjgl index: %s", data)
	}
	detail, err := LWJGLVersion(layout, "http://mirror", UIDLWJGL3, "3.3.3")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(detail, &doc); err != nil {
		t.Fatal(err)
	}
	libs, _ := doc["libraries"].([]any)
	if len(libs) != 2 {
		t.Fatalf("expected 2 lwjgl libs, got %v", libs)
	}
	names := []string{libs[0].(map[string]any)["name"].(string), libs[1].(map[string]any)["name"].(string)}
	want := map[string]bool{"org.lwjgl:lwjgl:3.3.3": true, "org.lwjgl:lwjgl-natives-linux:3.3.3": true}
	for _, n := range names {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Fatalf("missing flattened names %v in %v", want, names)
	}
	var natives map[string]any
	for _, l := range libs {
		m := l.(map[string]any)
		if m["name"] == "org.lwjgl:lwjgl-natives-linux:3.3.3" {
			natives = m
		}
	}
	if natives == nil {
		t.Fatalf("no flattened natives entry in %v", names)
	}
	natURL := natives["downloads"].(map[string]any)["artifact"].(map[string]any)["url"]
	if natURL != "http://mirror/libraries/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-linux.jar" {
		t.Fatalf("natives url not rewritten: %v", natURL)
	}
	conflicts, _ := doc["conflicts"].([]any)
	if len(conflicts) != 1 || conflicts[0].(map[string]any)["uid"] != UIDLWJGL2 {
		t.Fatalf("bad conflicts: %v", doc["conflicts"])
	}
	if _, err := LWJGLVersion(layout, "http://mirror", UIDLWJGL3, "9.9.9"); err == nil {
		t.Fatal("expected error for unknown lwjgl version")
	}
}

func TestIndexJSONIncludesComponents(t *testing.T) {
	layout := writeTestVersion(t)
	data, err := IndexJSON(layout, "http://mirror")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Packages []map[string]string `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	uids := map[string]string{}
	for _, p := range doc.Packages {
		uids[p["uid"]] = p["sha256"]
	}
	for _, uid := range []string{UIDMinecraft, UIDLWJGL3, UIDFabricLoader, UIDIntermediary} {
		if _, ok := uids[uid]; !ok {
			t.Fatalf("missing package %s in %v", uid, uids)
		}
	}
	if uids[UIDMinecraft] == "" || uids[UIDLWJGL3] == "" {
		t.Fatal("expected sha256 on generated components")
	}
	if _, ok := uids[UIDJava]; ok {
		t.Fatal("net.minecraft.java should be absent without temurin data")
	}
}
