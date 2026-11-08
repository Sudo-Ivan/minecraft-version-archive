package coordinator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/vfs"
)

type Claim struct {
	Agent   string `json:"agent"`
	Version string `json:"version"`
	Started int64  `json:"started"`
	PID     int    `json:"pid"`
}

// doneCacheTTL bounds how stale the done-marker view can be. Other agents'
// completions become visible after at most this delay.
const doneCacheTTL = 2 * time.Second

type Coordinator struct {
	fs        vfs.FS
	claimsDir string
	doneDir   string
	agent     string
	staleSec  int
	mu        sync.Mutex

	doneMu    sync.RWMutex
	doneCache map[string]bool
	doneAt    time.Time
}

// New creates a Coordinator over fs. fs must be rooted at an ancestor of
// root (typically the layout root); root is the coordinator directory that
// holds claims/ and done/.
func New(fsys vfs.FS, root, agent string, staleSec int) (*Coordinator, error) {
	if agent == "" {
		agent = defaultAgent()
	}
	if staleSec < 1 {
		staleSec = 3600
	}
	root = filepath.Clean(root)
	claimsDir := filepath.Join(root, "claims")
	doneDir := filepath.Join(root, "done")
	if err := fsys.MkdirAll(claimsDir, safepath.DirPerm); err != nil {
		return nil, err
	}
	if err := fsys.MkdirAll(doneDir, safepath.DirPerm); err != nil {
		return nil, err
	}
	return &Coordinator{
		fs:        fsys,
		claimsDir: claimsDir,
		doneDir:   doneDir,
		agent:     agent,
		staleSec:  staleSec,
	}, nil
}

// NewDir is a convenience constructor for a purely local coordinator root.
func NewDir(root, agent string, staleSec int) (*Coordinator, error) {
	fsys, err := safepath.OpenDir(root)
	if err != nil {
		return nil, err
	}
	return New(fsys, root, agent, staleSec)
}

func (c *Coordinator) IsDone(version string) bool {
	c.doneMu.RLock()
	fresh := c.doneCache != nil && time.Since(c.doneAt) < doneCacheTTL
	if fresh {
		done := c.doneCache[safeName(version)]
		c.doneMu.RUnlock()
		return done
	}
	c.doneMu.RUnlock()
	set, err := c.doneSet()
	if err != nil {
		return false
	}
	name := safeName(version)
	c.doneMu.Lock()
	c.doneCache = set
	c.doneAt = time.Now()
	c.doneMu.Unlock()
	_, done := set[name]
	return done
}

// doneSet lists the done directory once; on object stores this is a single
// prefix listing instead of one stat per version.
func (c *Coordinator) doneSet() (map[string]bool, error) {
	entries, err := c.fs.ReadDir(c.doneDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			set[e.Name()] = true
		}
	}
	return set, nil
}

func (c *Coordinator) MarkDone(version string) error {
	if err := c.fs.WriteFile(c.donePath(version), []byte("ok\n"), safepath.FilePerm); err != nil {
		return err
	}
	c.doneMu.Lock()
	if c.doneCache != nil {
		c.doneCache[safeName(version)] = true
	}
	c.doneMu.Unlock()
	return nil
}

func (c *Coordinator) ClaimNext(versions []string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, id := range versions {
		if c.IsDone(id) {
			continue
		}
		if c.tryClaim(id) {
			return id, true
		}
	}
	return "", false
}

func (c *Coordinator) Release(version string) {
	_ = c.fs.Remove(c.claimPath(version))
}

func (c *Coordinator) ActiveClaims() ([]Claim, error) {
	entries, err := c.fs.ReadDir(c.claimsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Claim
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(c.claimsDir, e.Name())
		data, err := c.fs.ReadFile(path)
		if err != nil {
			continue
		}
		var cl Claim
		if json.Unmarshal(data, &cl) == nil {
			out = append(out, cl)
		}
	}
	return out, nil
}

func (c *Coordinator) tryClaim(version string) bool {
	if c.IsDone(version) {
		return false
	}
	path := c.claimPath(version)
	if err := c.writeClaim(path, version); err == nil {
		return true
	}
	if err := c.clearStale(path); err != nil {
		return false
	}
	return c.writeClaim(path, version) == nil
}

func (c *Coordinator) writeClaim(path, version string) error {
	cl := Claim{
		Agent:   c.agent,
		Version: version,
		Started: time.Now().Unix(),
		PID:     os.Getpid(),
	}
	data, _ := json.Marshal(cl)
	return c.fs.WriteFileExclusive(path, append(data, '\n'), safepath.FilePerm)
}

func (c *Coordinator) clearStale(path string) error {
	data, err := c.fs.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var cl Claim
	if err := json.Unmarshal(data, &cl); err != nil {
		return nil
	}
	if time.Now().Unix()-cl.Started >= int64(c.staleSec) {
		_ = c.fs.Remove(path)
	}
	return nil
}

func (c *Coordinator) claimPath(version string) string {
	return filepath.Join(c.claimsDir, safeName(version)+".claim")
}

func (c *Coordinator) donePath(version string) string {
	return filepath.Join(c.doneDir, safeName(version))
}

func safeName(version string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "").Replace(version)
}

func defaultAgent() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "host"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func Pending(versions []string, coord *Coordinator) []string {
	out := make([]string, 0, len(versions))
	for _, id := range versions {
		if !coord.IsDone(id) {
			out = append(out, id)
		}
	}
	return out
}

func ParseAgentID() string {
	if v := os.Getenv("MVA_AGENT_ID"); v != "" {
		return v
	}
	return defaultAgent()
}

func FormatClaimSummary(claims []Claim) string {
	if len(claims) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(claims))
	for _, c := range claims {
		parts = append(parts, c.Version+"@"+c.Agent)
	}
	return strings.Join(parts, ", ")
}
