package server

import (
	"net/http"
	"strings"
)

var scannerPathMarkers = []string{
	"/.env",
	"/.git",
	"/.svn",
	"/.hg",
	"/.aws",
	"/.ssh",
	"/.docker",
	"/.vscode",
	"/.idea",
	"/wp-admin",
	"/wp-login",
	"/wp-content",
	"/wp-includes",
	"/wordpress",
	"/phpmyadmin",
	"/pma/",
	"/admin.php",
	"/config.php",
	"/database.yml",
	"/server-status",
	"/server-info",
	"/actuator",
	"/swagger",
	"/api-docs",
	"/graphql",
	"/xmlrpc.php",
	"/vendor/phpunit",
	"/cgi-bin",
	"/shell.php",
	"/cmd.php",
	"/eval.php",
	"/setup.php",
	"/install.php",
	"/backup.sql",
	"/dump.sql",
	"/.htaccess",
	"/.htpasswd",
	"/crossdomain.xml",
	"/clientaccesspolicy.xml",
	"/autodiscover",
	"/telescope",
	"/debug/",
	"/trace/",
	"/_profiler",
	"/elmah.axd",
	"/.well-known/security.txt",
}

var scannerExactPaths = map[string]struct{}{
	"/.env":               {},
	"/.git":               {},
	"/.git/config":        {},
	"/.git/head":          {},
	"/.gitignore":         {},
	"/login":              {},
	"/admin":              {},
	"/manager/html":       {},
	"/phpinfo.php":        {},
	"/info.php":           {},
	"/test.php":           {},
	"/aws.yml":            {},
	"/docker-compose.yml": {},
}

func isScannerRequest(r *http.Request) bool {
	path := r.URL.Path
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	if _, ok := scannerExactPaths[lower]; ok {
		return true
	}
	for _, marker := range scannerPathMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.HasSuffix(lower, ".env") || strings.HasSuffix(lower, ".sql") {
		return true
	}
	if strings.Contains(lower, "/.env.") || strings.Contains(lower, "/.git/") {
		return true
	}
	seg := strings.SplitSeq(strings.Trim(lower, "/"), "/")
	for s := range seg {
		switch {
		case s == ".git", s == ".svn", s == ".hg", s == ".env":
			return true
		case strings.HasPrefix(s, ".env"):
			return true
		case strings.HasPrefix(s, "wp-"):
			return true
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !strings.HasPrefix(lower, "/p2p/") {
			return true
		}
	}
	return false
}

func (s *Server) blockScanners(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isScannerRequest(r) {
			s.stats.RecordBlocked()
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
