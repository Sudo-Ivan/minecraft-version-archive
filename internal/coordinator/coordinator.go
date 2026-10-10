package coordinator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"minecraft-version-archive/internal/safepath"
)

type Claim struct {
	Agent   string `json:"agent"`
	Version string `json:"version"`
	Started int64  `json:"started"`
	PID     int    `json:"pid"`
}

type Coordinator struct {
	dir       *safepath.Dir
	claimsDir string
	doneDir   string
	agent     string
	staleSec  int
	mu        sync.Mutex
}

func New(root, agent string, staleSec int) (*Coordinator, error) {
	if agent == "" {
		agent = defaultAgent()
	}
	if staleSec < 1 {
		staleSec = 3600
	}
	root = filepath.Clean(root)
	dir, err := safepath.OpenDir(root)
	if err != nil {
		return nil, err
	}
	claimsDir := filepath.Join(root, "claims")
	doneDir := filepath.Join(root, "done")
	if err := dir.MkdirAll(claimsDir, safepath.DirPerm); err != nil {
		return nil, err
	}
	if err := dir.MkdirAll(doneDir, safepath.DirPerm); err != nil {
		return nil, err
	}
	return &Coordinator{
		dir:       dir,
		claimsDir: claimsDir,
		doneDir:   doneDir,
		agent:     agent,
		staleSec:  staleSec,
	}, nil
}

func (c *Coordinator) IsDone(version string) bool {
	return c.dir.Exists(c.donePath(version))
}

func (c *Coordinator) MarkDone(version string) error {
	return c.dir.WriteFile(c.donePath(version), []byte("ok\n"), safepath.FilePerm)
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
	_ = os.Remove(c.claimPath(version))
}

func (c *Coordinator) ActiveClaims() ([]Claim, error) {
	entries, err := os.ReadDir(c.claimsDir)
	if err != nil {
		return nil, err
	}
	var out []Claim
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(c.claimsDir, e.Name())
		data, err := c.dir.ReadFile(path)
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
	f, err := c.dir.OpenFileFlags(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, safepath.FilePerm)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	cl := Claim{
		Agent:   c.agent,
		Version: version,
		Started: time.Now().Unix(),
		PID:     os.Getpid(),
	}
	data, _ := json.Marshal(cl)
	_, err = f.Write(append(data, '\n'))
	return err
}

func (c *Coordinator) clearStale(path string) error {
	data, err := c.dir.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cl Claim
	if err := json.Unmarshal(data, &cl); err != nil {
		return nil
	}
	if time.Now().Unix()-cl.Started >= int64(c.staleSec) {
		_ = os.Remove(path)
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
