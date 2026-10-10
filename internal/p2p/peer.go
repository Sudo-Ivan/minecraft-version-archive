package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"minecraft-version-archive/internal/fetch"
)

type Inventory struct {
	Versions  []string `json:"versions"`
	Libraries []string `json:"libraries"`
	Assets    []string `json:"assets"`
}

type Peer struct {
	BaseURL string
	client  *fetch.Client
}

type Registry struct {
	peers  []*Peer
	client *fetch.Client
}

func NewRegistry(peerURLs []string, timeoutSec, retryMax, baseMS int) *Registry {
	peers := make([]*Peer, 0, len(peerURLs))
	client := fetch.NewWithHTTP(
		fetch.SharedPeerHTTPClient(time.Duration(timeoutSec)*time.Second),
		retryMax,
		baseMS,
	)
	for _, u := range peerURLs {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u == "" {
			continue
		}
		peers = append(peers, &Peer{
			BaseURL: u,
			client:  client,
		})
	}
	return &Registry{peers: peers, client: client}
}

func (r *Registry) Peers() []*Peer {
	return r.peers
}

func (p *Peer) Inventory(ctx context.Context) (*Inventory, error) {
	data, err := p.client.Get(ctx, p.BaseURL+"/p2p/inventory")
	if err != nil {
		return nil, err
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

func (p *Peer) HasVersion(ctx context.Context, id string) (bool, error) {
	inv, err := p.Inventory(ctx)
	if err != nil {
		return false, err
	}
	if slices.Contains(inv.Versions, id) {
		return true, nil
	}
	return false, nil
}

func (p *Peer) Download(ctx context.Context, remotePath, dest string) error {
	url := p.BaseURL + remotePath
	return p.client.Download(ctx, url, dest)
}

func (r *Registry) FindVersion(ctx context.Context, id string) (*Peer, error) {
	for _, p := range r.peers {
		ok, err := p.HasVersion(ctx, id)
		if err != nil {
			continue
		}
		if ok {
			return p, nil
		}
	}
	return nil, fmt.Errorf("version %q not found on any peer", id)
}

func (r *Registry) FetchVersionJSON(ctx context.Context, peer *Peer, id, dest string) error {
	return peer.Download(ctx, "/versions/"+id+".json", dest)
}

func (r *Registry) FetchClientJar(ctx context.Context, peer *Peer, id, dest string) error {
	return peer.Download(ctx, "/jars/client/"+id+".jar", dest)
}

func (r *Registry) FetchServerJar(ctx context.Context, peer *Peer, id, dest string) error {
	return peer.Download(ctx, "/jars/server/"+id+".jar", dest)
}

func (r *Registry) FetchLibrary(ctx context.Context, peer *Peer, artifactPath, dest string) error {
	return peer.Download(ctx, "/libraries/"+artifactPath, dest)
}

func (r *Registry) FetchAssetIndex(ctx context.Context, peer *Peer, id, dest string) error {
	return peer.Download(ctx, "/assets/indexes/"+id+".json", dest)
}

func (r *Registry) FetchAssetObject(ctx context.Context, peer *Peer, hash, dest string) error {
	if len(hash) < 2 {
		return fmt.Errorf("invalid asset hash")
	}
	return peer.Download(ctx, "/assets/objects/"+hash[:2]+"/"+hash, dest)
}

func WriteInventory(w io.Writer, inv Inventory) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}
