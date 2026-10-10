package mojang

import (
	"fmt"
	"strings"
)

func ResolveVersionID(requested string, versions []ManifestVersion) (string, error) {
	known := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		known[v.ID] = struct{}{}
	}
	if _, ok := known[requested]; ok {
		return requested, nil
	}
	for _, alias := range aliasCandidates(requested) {
		if _, ok := known[alias]; ok {
			return alias, nil
		}
	}
	suggestions := SuggestVersionIDs(requested, versions, 5)
	if len(suggestions) == 0 {
		return "", fmt.Errorf("unknown version %q", requested)
	}
	return "", fmt.Errorf("unknown version %q (did you mean: %s)", requested, strings.Join(suggestions, ", "))
}

func aliasCandidates(id string) []string {
	var out []string
	cur := id
	for strings.HasSuffix(cur, ".0") {
		cur = strings.TrimSuffix(cur, ".0")
		if cur == "" {
			break
		}
		out = append(out, cur)
	}
	return out
}

func SuggestVersionIDs(requested string, versions []ManifestVersion, limit int) []string {
	if limit <= 0 {
		return nil
	}
	lower := strings.ToLower(requested)
	var exactPrefix []string
	var contains []string
	for _, v := range versions {
		id := v.ID
		lid := strings.ToLower(id)
		switch {
		case lid == lower:
			return []string{id}
		case strings.HasPrefix(lid, lower):
			exactPrefix = append(exactPrefix, id)
		case strings.Contains(lid, lower):
			contains = append(contains, id)
		}
	}
	out := append(exactPrefix, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func ResolveVersionIDs(requested []string, versions []ManifestVersion) ([]string, error) {
	out := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		resolved, err := ResolveVersionID(id, versions)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		out = append(out, resolved)
	}
	return out, nil
}
