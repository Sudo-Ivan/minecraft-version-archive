package mojang

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/fetch"
	"minecraft-version-archive/internal/stats"
)

type Manifest struct {
	Latest   Latest            `json:"latest"`
	Versions []ManifestVersion `json:"versions"`
}

type Latest struct {
	Release  string `json:"release"`
	Snapshot string `json:"snapshot"`
}

type ManifestVersion struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	URL             string `json:"url"`
	Time            string `json:"time"`
	ReleaseTime     string `json:"releaseTime"`
	SHA1            string `json:"sha1"`
	ComplianceLevel int    `json:"complianceLevel"`
}

type Version struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Time        string          `json:"time"`
	ReleaseTime string          `json:"releaseTime"`
	Downloads   Downloads       `json:"downloads"`
	AssetIndex  AssetIndexRef   `json:"assetIndex"`
	Assets      string          `json:"assets"`
	Libraries   []Library       `json:"libraries"`
	JavaVersion *JavaVersion    `json:"javaVersion,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

type Downloads struct {
	Client *Artifact `json:"client,omitempty"`
	Server *Artifact `json:"server,omitempty"`
}

type Artifact struct {
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type AssetIndexRef struct {
	ID        string `json:"id"`
	SHA1      string `json:"sha1"`
	Size      int64  `json:"size"`
	TotalSize int64  `json:"totalSize"`
	URL       string `json:"url"`
}

type Library struct {
	Name      string           `json:"name,omitempty"`
	Downloads LibraryDownloads `json:"downloads"`
	Rules     []Rule           `json:"rules,omitempty"`
}

type LibraryDownloads struct {
	Artifact    *Artifact           `json:"artifact,omitempty"`
	Classifiers map[string]Artifact `json:"classifiers,omitempty"`
}

type Rule struct {
	Action string  `json:"action"`
	OS     *RuleOS `json:"os,omitempty"`
}

type RuleOS struct {
	Name string `json:"name,omitempty"`
}

type JavaVersion struct {
	Component    string `json:"component"`
	MajorVersion int    `json:"majorVersion"`
}

type AssetIndex struct {
	Objects map[string]AssetObject `json:"objects"`
}

type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type Client struct {
	fetch *fetch.Client
}

func NewClient(retryMax, baseMS int) *Client {
	return &Client{fetch: fetch.New(retryMax, baseMS)}
}

func NewClientWithFetch(c *fetch.Client) *Client {
	return &Client{fetch: c}
}

func (c *Client) WithStats(s *stats.Collector) *Client {
	c.fetch = c.fetch.WithStats(s)
	return c
}

func (c *Client) FetchBytes(ctx context.Context, url string) ([]byte, error) {
	return c.fetch.Get(ctx, url)
}

func (c *Client) FetchManifest(ctx context.Context) (*Manifest, error) {
	data, err := c.fetch.Get(ctx, constants.MojangManifest)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *Client) FetchVersion(ctx context.Context, url string) (*Version, []byte, error) {
	data, err := c.fetch.Get(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	var v Version
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, nil, err
	}
	v.Raw = append(json.RawMessage(nil), data...)
	return &v, data, nil
}

func (c *Client) FetchAssetIndex(ctx context.Context, url string) (*AssetIndex, error) {
	data, err := c.fetch.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var idx AssetIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func LibraryArtifactPath(lib Library) string {
	if lib.Downloads.Artifact == nil {
		return ""
	}
	url := lib.Downloads.Artifact.URL
	if url == "" {
		return ""
	}
	prefix := constants.MojangLibBase + "/"
	if after, ok := strings.CutPrefix(url, prefix); ok {
		return after
	}
	if i := strings.Index(url, constants.MojangLibHost); i >= 0 {
		return url[i+len(constants.MojangLibHost):]
	}
	return ""
}

func RewriteVersionJSON(raw []byte, publicURL string, versionID string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	base := strings.TrimRight(publicURL, "/")

	if downloads, ok := doc["downloads"].(map[string]any); ok {
		for kind := range downloads {
			if item, ok := downloads[kind].(map[string]any); ok {
				item["url"] = fmt.Sprintf("%s/jars/%s/%s.jar", base, kind, versionID)
			}
		}
	}
	if assetIndex, ok := doc["assetIndex"].(map[string]any); ok {
		if id, ok := assetIndex["id"].(string); ok {
			assetIndex["url"] = fmt.Sprintf("%s/assets/indexes/%s.json", base, id)
		}
	}

	if libs, ok := doc["libraries"].([]any); ok {
		for _, entry := range libs {
			lib, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			downloads, ok := lib["downloads"].(map[string]any)
			if !ok {
				continue
			}
			if artifact, ok := downloads["artifact"].(map[string]any); ok {
				rewriteArtifactURL(artifact, base)
			}
			if classifiers, ok := downloads["classifiers"].(map[string]any); ok {
				for _, v := range classifiers {
					if art, ok := v.(map[string]any); ok {
						rewriteArtifactURL(art, base)
					}
				}
			}
		}
	}

	return json.MarshalIndent(doc, "", "  ")
}

func rewriteArtifactURL(artifact map[string]any, base string) {
	url, ok := artifact["url"].(string)
	if !ok || url == "" {
		return
	}
	path := ""
	libPrefix := constants.MojangLibBase + "/"
	if after, ok0 := strings.CutPrefix(url, libPrefix); ok0 {
		path = after
	} else if i := strings.Index(url, constants.MojangLibHost); i >= 0 {
		path = url[i+len(constants.MojangLibHost):]
	}
	if path != "" {
		artifact["url"] = base + "/libraries/" + path
	}
}

func RewriteManifest(m *Manifest, publicURL string) *Manifest {
	out := *m
	out.Versions = make([]ManifestVersion, len(m.Versions))
	base := strings.TrimRight(publicURL, "/")
	for i, v := range m.Versions {
		out.Versions[i] = v
		out.Versions[i].URL = fmt.Sprintf("%s/versions/%s.json", base, v.ID)
	}
	return &out
}
