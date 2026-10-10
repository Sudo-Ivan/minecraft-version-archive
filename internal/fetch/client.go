package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"minecraft-version-archive/internal/constants"
	"minecraft-version-archive/internal/integrity"
	"minecraft-version-archive/internal/safepath"
	"minecraft-version-archive/internal/stats"
)

type fileLock struct {
	mu   sync.Mutex
	refs int
}

type Client struct {
	http    *http.Client
	retry   int
	baseMS  int
	headers map[string]string
	locksMu sync.Mutex
	locks   map[string]*fileLock
	stats   *stats.Collector
	data    *safepath.Dir
}

func New(retryMax, baseMS int) *Client {
	return &Client{
		http:   sharedHTTPClient(constants.FetchTimeout),
		retry:  retryMax,
		baseMS: baseMS,
		locks:  make(map[string]*fileLock),
	}
}

func NewWithHTTP(httpClient *http.Client, retryMax, baseMS int) *Client {
	if httpClient == nil {
		return New(retryMax, baseMS)
	}
	return &Client{
		http:   httpClient,
		retry:  retryMax,
		baseMS: baseMS,
		locks:  make(map[string]*fileLock),
	}
}

func (c *Client) WithDataDir(dir *safepath.Dir) *Client {
	c.data = dir
	return c
}

func (c *Client) WithStats(s *stats.Collector) *Client {
	c.stats = s
	return c
}

func (c *Client) WithHeader(key, value string) *Client {
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	c.headers[key] = value
	return c
}

func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.applyHeaders(req)

	var last error
	for attempt := 0; attempt < c.retry; attempt++ {
		if attempt > 0 {
			delay := time.Duration(c.baseMS*(1<<uint(attempt-1))) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, constants.MaxGetBodyBytes+1))
		_ = resp.Body.Close()
		if int64(len(body)) > constants.MaxGetBodyBytes {
			return nil, fmt.Errorf("GET %s: body exceeds %d bytes", url, constants.MaxGetBodyBytes)
		}
		if resp.StatusCode == http.StatusOK {
			if c.stats != nil {
				c.stats.RecordArchiveDownload(int64(len(body)))
			}
			return body, readErr
		}
		last = fmt.Errorf("GET %s: %s", url, resp.Status)
		if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return nil, last
		}
	}
	return nil, last
}

// Do issues a GET for url with retry and returns the final response with its
// body still open. The caller must close resp.Body. Responses with a status
// below 500 (except 429) are returned as-is so callers can relay them.
func (c *Client) Do(ctx context.Context, url string, fwd http.Header) (*http.Response, error) {
	var last error
	for attempt := 0; attempt < c.retry; attempt++ {
		if attempt > 0 {
			delay := time.Duration(c.baseMS*(1<<uint(attempt-1))) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		c.applyHeaders(req)
		for k, vv := range fwd {
			for _, v := range vv {
				req.Header.Set(k, v)
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			last = fmt.Errorf("GET %s: %s", url, resp.Status)
			continue
		}
		return resp, nil
	}
	return nil, last
}

func (c *Client) acquireLock(dest string) *fileLock {
	c.locksMu.Lock()
	l := c.locks[dest]
	if l == nil {
		l = &fileLock{}
		c.locks[dest] = l
	}
	l.refs++
	c.locksMu.Unlock()
	l.mu.Lock()
	return l
}

func (c *Client) releaseLock(dest string, l *fileLock) {
	l.mu.Unlock()
	c.locksMu.Lock()
	l.refs--
	if l.refs == 0 {
		delete(c.locks, dest)
	}
	c.locksMu.Unlock()
}

func (c *Client) Download(ctx context.Context, url, dest string) error {
	l := c.acquireLock(dest)
	defer c.releaseLock(dest, l)
	return c.downloadLocked(ctx, url, dest)
}

// downloadLocked runs one resumable download to dest. The caller must hold
// the per-dest lock via acquireLock.
func (c *Client) downloadLocked(ctx context.Context, url, dest string) error {
	if c.data != nil {
		if c.data.Exists(dest) {
			return nil
		}
	} else if _, err := os.Stat(dest); err == nil {
		return nil
	}

	tmp := dest + constants.FilePartSuffix
	offset := int64(0)
	if c.data != nil {
		if st, err := c.data.Stat(tmp); err == nil {
			offset = st.Size()
		}
	} else if st, err := os.Stat(tmp); err == nil {
		offset = st.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.applyHeaders(req)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	var last error
	for attempt := 0; attempt < c.retry; attempt++ {
		if attempt > 0 {
			delay := time.Duration(c.baseMS*(1<<uint(attempt-1))) * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}

		switch resp.StatusCode {
		case http.StatusOK:
			offset = 0
			err = c.writeBody(resp.Body, tmp, offset, true)
		case http.StatusPartialContent:
			err = c.writeBody(resp.Body, tmp, offset, false)
		case http.StatusRequestedRangeNotSatisfiable:
			_ = resp.Body.Close()
			offset = 0
			_ = os.Remove(tmp)
			req.Header.Del("Range")
			continue
		default:
			_ = resp.Body.Close()
			last = fmt.Errorf("GET %s: %s", url, resp.Status)
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return last
			}
			continue
		}
		_ = resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if c.stats != nil {
			if st, err := c.statPath(tmp); err == nil {
				c.stats.RecordArchiveDownload(st.Size())
			}
		}
		return c.renameTmp(tmp, dest)
	}
	return last
}

func (c *Client) DownloadVerified(ctx context.Context, url, dest string, spec integrity.Spec) error {
	if !spec.Known() {
		return c.Download(ctx, url, dest)
	}
	if c.data == nil {
		return fmt.Errorf("verified download requires data directory")
	}
	l := c.acquireLock(dest)
	defer c.releaseLock(dest, l)

	// A concurrent caller may have already fetched and verified dest.
	if err := integrity.VerifyFile(c.data, dest, spec); err == nil {
		return nil
	}

	var last error
	for attempt := 0; attempt < c.retry; attempt++ {
		_ = os.Remove(dest)
		if err := c.downloadLocked(ctx, url, dest); err != nil {
			last = err
			continue
		}
		if err := integrity.VerifyFile(c.data, dest, spec); err != nil {
			_ = os.Remove(dest)
			last = fmt.Errorf("integrity: %w", err)
			if attempt > 0 {
				delay := time.Duration(c.baseMS*(1<<uint(attempt-1))) * time.Millisecond
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
			continue
		}
		return nil
	}
	return last
}

var copyBufPool = sync.Pool{New: func() any { b := make([]byte, 256<<10); return &b }}

func (c *Client) writeBody(r io.Reader, path string, offset int64, truncate bool) error {
	if c.data == nil {
		return fmt.Errorf("download requires data directory")
	}
	var f *os.File
	var err error
	if truncate || offset == 0 {
		f, err = c.data.Create(path)
	} else {
		f, err = c.data.OpenFileFlags(path, os.O_WRONLY|os.O_APPEND, safepath.FilePerm)
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	bufp := copyBufPool.Get().(*[]byte)
	_, err = io.CopyBuffer(f, r, *bufp)
	copyBufPool.Put(bufp)
	return err
}

func (c *Client) statPath(path string) (os.FileInfo, error) {
	if c.data != nil {
		return c.data.Stat(path)
	}
	return os.Stat(path)
}

func (c *Client) renameTmp(tmp, dest string) error {
	return os.Rename(tmp, dest)
}

func (c *Client) applyHeaders(req *http.Request) {
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
}
