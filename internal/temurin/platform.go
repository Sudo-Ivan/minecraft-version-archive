package temurin

import "fmt"

type Platform struct {
	ID        string
	OS        string
	Arch      string
	RuntimeOS string
}

var DefaultPlatforms = []Platform{
	{ID: "linux-x64", OS: "linux", Arch: "x64", RuntimeOS: "linux-x64"},
	{ID: "linux-aarch64", OS: "linux", Arch: "aarch64", RuntimeOS: "linux-arm64"},
	{ID: "windows-x64", OS: "windows", Arch: "x64", RuntimeOS: "windows-x64"},
	{ID: "mac-x64", OS: "mac", Arch: "x64", RuntimeOS: "osx-x64"},
	{ID: "mac-aarch64", OS: "mac", Arch: "aarch64", RuntimeOS: "osx-arm64"},
}

func ParsePlatforms(ids []string) ([]Platform, error) {
	if len(ids) == 0 {
		out := make([]Platform, len(DefaultPlatforms))
		copy(out, DefaultPlatforms)
		return out, nil
	}
	byID := make(map[string]Platform, len(DefaultPlatforms))
	for _, p := range DefaultPlatforms {
		byID[p.ID] = p
	}
	out := make([]Platform, 0, len(ids))
	for _, id := range ids {
		p, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown java platform %q", id)
		}
		out = append(out, p)
	}
	return out, nil
}

func PlatformIDs(platforms []Platform) []string {
	out := make([]string, len(platforms))
	for i, p := range platforms {
		out[i] = p.ID
	}
	return out
}
