package fetch

import (
	"testing"
	"time"
)

func TestSharedTransportReuse(t *testing.T) {
	a := sharedHTTPClient(time.Minute)
	b := sharedHTTPClient(time.Minute)
	if a.Transport != b.Transport {
		t.Fatal("expected shared transport instance")
	}
}

func TestSharedTransportPoolingDefaults(t *testing.T) {
	tr := sharedTransport()
	if tr.MaxIdleConns != maxIdleConns {
		t.Fatalf("MaxIdleConns = %d", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != maxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d", tr.MaxIdleConnsPerHost)
	}
}
