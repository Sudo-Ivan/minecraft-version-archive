package p2p

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegistryInventoryAndDownload(t *testing.T) {
	inv := Inventory{Versions: []string{"1.21.1"}, Libraries: []string{"a"}, Assets: []string{"b"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p2p/inventory":
			_ = json.NewEncoder(w).Encode(inv)
		case "/versions/1.21.1.json":
			_, _ = w.Write([]byte(`{"id":"1.21.1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reg := NewRegistry([]string{srv.URL}, 5, 2, 10)
	peer := reg.Peers()[0]
	got, err := peer.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 1 || got.Versions[0] != "1.21.1" {
		t.Fatalf("inv = %+v", got)
	}
	ok, err := peer.HasVersion(context.Background(), "1.21.1")
	if err != nil || !ok {
		t.Fatalf("has = %v err=%v", ok, err)
	}
}
