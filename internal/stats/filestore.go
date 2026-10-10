package stats

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/safepath"
)

const statsDirName = "stats"

type FileStore struct {
	dir     string
	history string
}

func NewFileStore(dataDir string) (*FileStore, error) {
	dir := filepath.Join(dataDir, statsDirName)
	if err := os.MkdirAll(dir, safepath.DirPerm); err != nil {
		return nil, err
	}
	return &FileStore{
		dir:     dir,
		history: filepath.Join(dir, "history.log"),
	}, nil
}

func (f *FileStore) AppendHistory(line string, at time.Time) error {
	fd, err := os.OpenFile(f.history, os.O_CREATE|os.O_WRONLY|os.O_APPEND, safepath.FilePerm)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(fd, "%s %s\n", at.UTC().Format(time.RFC3339), line)
	size := int64(-1)
	if st, statErr := fd.Stat(); statErr == nil {
		size = st.Size()
	}
	closeErr := fd.Close()
	if err != nil {
		return err
	}
	if size > constants.StatsHistoryMaxBytes {
		return f.rotateHistory()
	}
	return closeErr
}

// rotateHistory truncates history.log to its newest 75%, cutting at a line
// boundary so the file stays parseable.
func (f *FileStore) rotateHistory() error {
	data, err := os.ReadFile(f.history)
	if err != nil {
		return err
	}
	keep := constants.StatsHistoryMaxBytes * 3 / 4
	if len(data) <= keep {
		return nil
	}
	tail := data[len(data)-keep:]
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	tmp := f.history + ".tmp"
	if err := os.WriteFile(tmp, tail, safepath.FilePerm); err != nil { //#nosec G703 -- path is built from the trusted data dir
		return err
	}
	return os.Rename(tmp, f.history)
}
