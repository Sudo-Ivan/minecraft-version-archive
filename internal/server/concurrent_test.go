package server

import (
	"io"
	"net/http"
	"sync"
	"testing"
)

func TestConcurrentCachedDownloads(t *testing.T) {
	ts, _ := testServer(t, true)
	defer ts.Close()
	url := ts.URL + "/jars/client/1.0.jar"

	const workers = 128
	const perWorker = 16
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	for range workers {
		wg.Go(func() {
			for range perWorker {
				resp, err := http.Get(url)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return
				}
			}
		})
	}
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}
