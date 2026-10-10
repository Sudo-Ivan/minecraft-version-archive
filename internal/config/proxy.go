package config

import (
	"net/http"
	"strings"
)

func RequestPublicURL(r *http.Request, configured string, trustProxy bool) string {
	configured = strings.TrimRight(configured, "/")
	if !trustProxy {
		return configured
	}
	host := forwardedHost(r)
	if host == "" {
		return configured
	}
	return forwardedProto(r) + "://" + host
}

func forwardedProto(r *http.Request) string {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return strings.ToLower(strings.TrimSpace(strings.Split(p, ",")[0]))
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func forwardedHost(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		return strings.TrimSpace(strings.Split(h, ",")[0])
	}
	return strings.TrimSpace(r.Host)
}
