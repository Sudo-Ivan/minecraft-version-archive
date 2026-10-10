package polymc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/store"
)

const (
	UIDMinecraft = "net.minecraft"
	UIDLWJGL3    = "org.lwjgl3"
	UIDLWJGL2    = "org.lwjgl"
	UIDJava      = "net.minecraft.java"
)

// lwjglGroup converts a Mojang library coordinate into its PolyMC component
// uid when it is an LWJGL artifact. Mojang names LWJGL 3 libs org.lwjgl:* and
// LWJGL 2 libs org.lwjgl.lwjgl:*.
func lwjglUID(name string) string {
	if strings.HasPrefix(name, "org.lwjgl:") {
		return UIDLWJGL3
	}
	if strings.HasPrefix(name, "org.lwjgl.lwjgl:") {
		return UIDLWJGL2
	}
	return ""
}

// lwjglVersion returns the LWJGL version a Mojang version JSON depends on.
func lwjglVersion(libs []any) string {
	for _, e := range libs {
		lib, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _ := lib["name"].(string)
		if lwjglUID(name) == "" {
			continue
		}
		parts := strings.Split(name, ":")
		if len(parts) >= 3 {
			return parts[2]
		}
	}
	return ""
}

// lwjglRequires returns the requires clause for a component that pulls in the
// given LWJGL version, matching upstream meta output.
func lwjglRequires(version string) []Require {
	if version == "" {
		return nil
	}
	uid := UIDLWJGL3
	if strings.HasPrefix(version, "2.") {
		uid = UIDLWJGL2
	}
	return []Require{{UID: uid, Suggests: version}}
}

// mcVersionIDs lists archived Minecraft version ids.
func mcVersionIDs(layout *store.Layout) ([]string, error) {
	entries, err := os.ReadDir(layout.VersionsDir())
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(ent.Name(), ".json"))
	}
	return ids, nil
}

func readVersionDoc(layout *store.Layout, id string) (map[string]any, error) {
	data, err := layout.ReadFile(layout.VersionJSON(id))
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// rewriteLibURL points a Mojang library artifact url at the local mirror.
func rewriteLibURL(artifact map[string]any, base string) {
	url, ok := artifact["url"].(string)
	if !ok || url == "" {
		return
	}
	prefix := constants.MojangLibBase + "/"
	if after, ok := strings.CutPrefix(url, prefix); ok {
		artifact["url"] = base + "/libraries/" + after
		return
	}
	if i := strings.Index(url, constants.MojangLibHost); i >= 0 {
		artifact["url"] = base + "/libraries" + url[i+len(constants.MojangLibHost):]
	}
}

func rewriteLibDownloads(lib map[string]any, base string) {
	downloads, ok := lib["downloads"].(map[string]any)
	if !ok {
		return
	}
	if art, ok := downloads["artifact"].(map[string]any); ok {
		rewriteLibURL(art, base)
	}
	if classifiers, ok := downloads["classifiers"].(map[string]any); ok {
		for _, c := range classifiers {
			if art, ok := c.(map[string]any); ok {
				rewriteLibURL(art, base)
			}
		}
	}
}

// gameFeatures returns the +traits entries implied by a Mojang arguments
// block: feature flags on conditional game args, plus FirstThreadOnMacOS
// when the jvm args request -XstartOnFirstThread.
func gameFeatures(doc map[string]any) []string {
	traits := map[string]bool{}
	args, _ := doc["arguments"].(map[string]any)
	for _, e := range listAny(args["game"]) {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		for _, r := range listAny(entry["rules"]) {
			rule, _ := r.(map[string]any)
			feats, _ := rule["features"].(map[string]any)
			for name := range feats {
				traits["feature:"+name] = true
			}
		}
	}
	for _, e := range listAny(args["jvm"]) {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		for _, v := range listAny(entry["value"]) {
			if s, ok := v.(string); ok && s == "-XstartOnFirstThread" {
				traits["FirstThreadOnMacOS"] = true
			}
		}
	}
	out := make([]string, 0, len(traits))
	for t := range traits {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func listAny(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// minecraftArguments builds the flat argument template from a Mojang
// arguments.game block, or copies the legacy minecraftArguments field.
func minecraftArguments(doc map[string]any) string {
	if s, ok := doc["minecraftArguments"].(string); ok {
		return s
	}
	args, _ := doc["arguments"].(map[string]any)
	var parts []string
	for _, e := range listAny(args["game"]) {
		if s, ok := e.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// MinecraftDetail converts an archived Mojang version JSON into a PolyMC
// net.minecraft component file with URLs rewritten to this mirror.
func MinecraftDetail(layout *store.Layout, version, publicURL string) ([]byte, error) {
	doc, err := readVersionDoc(layout, version)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(publicURL, "/")
	id, _ := doc["id"].(string)
	if id == "" {
		id = version
	}

	out := map[string]any{
		"formatVersion": 1,
		"uid":           UIDMinecraft,
		"name":          "Minecraft",
		"version":       id,
		"order":         -2,
		"+jvmArgs":      []string{},
	}
	for _, k := range []string{"type", "releaseTime", "time", "mainClass", "minimumLauncherVersion", "complianceLevel"} {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	// Mojang nests the client log4j config under logging.client; PolyMC meta
	// emits that object directly.
	if logging, ok := doc["logging"].(map[string]any); ok {
		if client, ok := logging["client"]; ok {
			out["logging"] = client
		}
	}
	if a := minecraftArguments(doc); a != "" {
		out["minecraftArguments"] = a
	}

	traits := gameFeatures(doc)
	var req []Require
	libs, _ := doc["libraries"].([]any)
	if ver := lwjglVersion(libs); ver != "" {
		req = lwjglRequires(ver)
		if strings.HasPrefix(ver, "3.") {
			traits = append(traits, "XR:Initial")
		}
	}
	if len(traits) > 0 {
		sort.Strings(traits)
		out["+traits"] = traits
	}
	if len(req) > 0 {
		out["requires"] = req
	}

	if jv, ok := doc["javaVersion"].(map[string]any); ok {
		if maj, ok := jv["majorVersion"].(float64); ok {
			out["compatibleJavaMajors"] = []int{int(maj)}
		}
		if comp, ok := jv["component"].(string); ok {
			out["compatibleJavaName"] = comp
		}
	}

	outLibs := make([]any, 0, len(libs))
	for _, e := range libs {
		lib, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _ := lib["name"].(string)
		if lwjglUID(name) != "" {
			continue
		}
		rewriteLibDownloads(lib, base)
		outLibs = append(outLibs, lib)
	}
	out["libraries"] = outLibs

	if downloads, ok := doc["downloads"].(map[string]any); ok {
		if client, ok := downloads["client"].(map[string]any); ok {
			art := map[string]any{}
			for _, k := range []string{"sha1", "size"} {
				if v, ok := client[k]; ok {
					art[k] = v
				}
			}
			art["url"] = fmt.Sprintf("%s/jars/client/%s.jar", base, id)
			out["mainJar"] = map[string]any{
				"name":      "com.mojang:minecraft:" + id + ":client",
				"downloads": map[string]any{"artifact": art},
			}
		}
	}
	if idx, ok := doc["assetIndex"].(map[string]any); ok {
		ai := map[string]any{}
		for _, k := range []string{"id", "sha1", "size", "totalSize"} {
			if v, ok := idx[k]; ok {
				ai[k] = v
			}
		}
		if idv, ok := idx["id"].(string); ok {
			ai["url"] = fmt.Sprintf("%s/assets/indexes/%s.json", base, idv)
		}
		out["assetIndex"] = ai
	}
	return json.MarshalIndent(out, "", "    ")
}

// MinecraftIndex builds the net.minecraft component index. Each entry carries
// the sha256 of its detail document like upstream meta does.
func MinecraftIndex(layout *store.Layout, publicURL string) ([]byte, error) {
	ids, err := mcVersionIDs(layout)
	if err != nil {
		return nil, err
	}
	type mcMeta struct {
		id          string
		typ         string
		releaseTime string
		requires    []Require
		detail      []byte
	}
	metas := make([]mcMeta, 0, len(ids))
	for _, id := range ids {
		doc, err := readVersionDoc(layout, id)
		if err != nil {
			continue
		}
		detail, err := MinecraftDetail(layout, id, publicURL)
		if err != nil {
			continue
		}
		libs, _ := doc["libraries"].([]any)
		metas = append(metas, mcMeta{
			id:          id,
			typ:         strField(doc, "type"),
			releaseTime: strField(doc, "releaseTime"),
			requires:    lwjglRequires(lwjglVersion(libs)),
			detail:      detail,
		})
	}
	sort.SliceStable(metas, func(i, j int) bool {
		return metas[i].releaseTime > metas[j].releaseTime
	})
	entries := make([]VersionEntry, 0, len(metas))
	for _, m := range metas {
		sum := sha256.Sum256(m.detail)
		entries = append(entries, VersionEntry{
			Version:     m.id,
			Type:        m.typ,
			ReleaseTime: m.releaseTime,
			Requires:    m.requires,
			SHA256:      hex.EncodeToString(sum[:]),
		})
	}
	return marshalVersionList(UIDMinecraft, "Minecraft", entries)
}

func strField(doc map[string]any, k string) string {
	s, _ := doc[k].(string)
	return s
}

// lwjglArtifact rewrites a Mojang library name g:a:v[:classifier] into the
// PolyMC form where the classifier folds into the artifact id.
func lwjglArtifact(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) == 4 {
		return parts[0] + ":" + parts[1] + "-" + parts[3] + ":" + parts[2]
	}
	return name
}

// LWJGLIndex lists every distinct LWJGL version referenced by archived
// Minecraft versions for the given component (2 or 3).
func LWJGLIndex(layout *store.Layout, publicURL string, uid string) ([]byte, error) {
	libs, times, err := collectLWJGL(layout, publicURL, uid)
	if err != nil {
		return nil, err
	}
	vers := make([]string, 0, len(libs))
	for v := range libs {
		vers = append(vers, v)
	}
	sort.Slice(vers, func(i, j int) bool { return compareVersion(vers[i], vers[j]) > 0 })
	name := "LWJGL 3"
	if uid == UIDLWJGL2 {
		name = "LWJGL 2"
	}
	entries := make([]VersionEntry, 0, len(vers))
	for _, v := range vers {
		body, err := marshalLWJGLVersion(uid, name, v, libs[v])
		if err != nil {
			continue
		}
		sum := sha256.Sum256(body)
		entries = append(entries, VersionEntry{
			Version:     v,
			Type:        "release",
			ReleaseTime: times[v],
			SHA256:      hex.EncodeToString(sum[:]),
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no lwjgl entries archived")
	}
	return marshalVersionList(uid, name, entries)
}

// LWJGLVersion emits a single LWJGL component version built from the union of
// LWJGL library entries found across archived Minecraft versions.
func LWJGLVersion(layout *store.Layout, publicURL, uid, version string) ([]byte, error) {
	libs, _, err := collectLWJGL(layout, publicURL, uid)
	if err != nil {
		return nil, err
	}
	entries, ok := libs[version]
	if !ok {
		return nil, fmt.Errorf("lwjgl %s not found in archived versions", version)
	}
	name := "LWJGL 3"
	if uid == UIDLWJGL2 {
		name = "LWJGL 2"
	}
	return marshalLWJGLVersion(uid, name, version, entries)
}

func marshalLWJGLVersion(uid, name, version string, libs []any) ([]byte, error) {
	out := map[string]any{
		"formatVersion": 1,
		"uid":           uid,
		"name":          name,
		"version":       version,
		"type":          "release",
		"order":         1,
		"libraries":     libs,
	}
	if uid == UIDLWJGL3 {
		out["conflicts"] = []map[string]string{{"uid": UIDLWJGL2}}
	}
	return json.MarshalIndent(out, "", "    ")
}

// collectLWJGL scans archived version JSONs and returns flattened LWJGL
// library entries keyed by LWJGL version plus the earliest MC releaseTime
// that referenced each version.
func collectLWJGL(layout *store.Layout, publicURL, uid string) (map[string][]any, map[string]string, error) {
	base := strings.TrimRight(publicURL, "/")
	ids, err := mcVersionIDs(layout)
	if err != nil {
		return nil, nil, err
	}
	libs := map[string][]any{}
	seen := map[string]map[string]bool{}
	times := map[string]string{}
	for _, id := range ids {
		doc, err := readVersionDoc(layout, id)
		if err != nil {
			continue
		}
		rel := strField(doc, "releaseTime")
		for _, e := range listAny(doc["libraries"]) {
			lib, ok := e.(map[string]any)
			if !ok {
				continue
			}
			name, _ := lib["name"].(string)
			if lwjglUID(name) != uid {
				continue
			}
			parts := strings.Split(name, ":")
			if len(parts) < 3 {
				continue
			}
			ver := parts[2]
			flat := map[string]any{}
			for k, v := range lib {
				if k == "name" {
					continue
				}
				flat[k] = v
			}
			flat["name"] = lwjglArtifact(name)
			rewriteLibDownloads(flat, base)
			if seen[ver] == nil {
				seen[ver] = map[string]bool{}
			}
			if seen[ver][flat["name"].(string)] {
				continue
			}
			seen[ver][flat["name"].(string)] = true
			libs[ver] = append(libs[ver], flat)
			if cur := times[ver]; cur == "" || rel < cur {
				times[ver] = rel
			}
		}
	}
	for _, l := range libs {
		sort.Slice(l, func(i, j int) bool {
			a, _ := l[i].(map[string]any)["name"].(string)
			b, _ := l[j].(map[string]any)["name"].(string)
			return a < b
		})
	}
	return libs, times, nil
}

// compareVersion does a numeric-aware descending comparison for dotted
// versions like 3.3.1 and 3.3.10.
func compareVersion(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := strings.Compare(numkey(as[i]), numkey(bs[i])); c != 0 {
			return c
		}
	}
	return len(as) - len(bs)
}

func numkey(s string) string {
	var num string
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num += string(r)
		} else {
			break
		}
	}
	return fmt.Sprintf("%09s%s", num, s[len(num):])
}
