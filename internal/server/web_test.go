package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func get(t *testing.T, url, accept string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestRootServesHTMLToBrowsers(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp := get(t, ts.URL+"/", "text/html,application/xhtml+xml")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	if vary := resp.Header.Values("Vary"); !strings.Contains(strings.Join(vary, ","), "Accept") {
		t.Fatalf("vary = %v", vary)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<html") {
		t.Fatal("expected html body")
	}
}

func TestRootServesJSONToAPIClients(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	for _, accept := range []string{"", "*/*", "application/json"} {
		resp := get(t, ts.URL+"/", accept)
		var doc struct {
			PublicURL string `json:"public_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			resp.Body.Close()
			t.Fatalf("accept %q: %v", accept, err)
		}
		resp.Body.Close()
		if doc.PublicURL != "http://test" {
			t.Fatalf("accept %q: public_url = %q", accept, doc.PublicURL)
		}
	}
}

func TestRootJSONOverride(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp := get(t, ts.URL+"/?json", "text/html")
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestDocsPage(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	resp := get(t, ts.URL+"/docs", "text/html")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "API reference") {
		t.Fatal("docs page missing title")
	}
}

func TestWebAssets(t *testing.T) {
	ts, _ := testServer(t, false)
	defer ts.Close()

	for path, wantCT := range map[string]string{
		"/web/site.css": "text/css",
		"/web/site.js":  "text/javascript",
	} {
		resp := get(t, ts.URL+path, "")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, wantCT) {
			t.Fatalf("%s: content-type = %q", path, ct)
		}
		if len(body) == 0 {
			t.Fatalf("%s: empty body", path)
		}
	}

	resp := get(t, ts.URL+"/web/fonts/bungee-latin-400-normal.woff2", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("font: status = %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("font cache-control = %q", cc)
	}

	resp = get(t, ts.URL+"/web/../settings.ini", "")
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("traversal served")
	}

	resp = get(t, ts.URL+"/web/nope.css", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset: status = %d", resp.StatusCode)
	}
}
