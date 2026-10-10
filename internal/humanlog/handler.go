package humanlog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

const (
	reset  = "\033[0m"
	dim    = "\033[2m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
)

type Handler struct {
	w      io.Writer
	color  bool
	isTTY  bool
	mu     sync.Mutex
	attrs  []slog.Attr
	groups []string
}

func New(w io.Writer) *Handler {
	isTTY := isTerminal(w)
	return &Handler{
		w:     w,
		color: isTTY && os.Getenv("NO_COLOR") == "",
		isTTY: isTTY,
	}
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	line, inplace := h.format(r)
	h.mu.Lock()
	defer h.mu.Unlock()
	if inplace && h.isTTY {
		_, err := fmt.Fprintf(h.w, "\r%s\033[K", line)
		return err
	}
	_, err := fmt.Fprintln(h.w, line)
	return err
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		w:      h.w,
		color:  h.color,
		isTTY:  h.isTTY,
		attrs:  append(append([]slog.Attr{}, h.attrs...), attrs...),
		groups: append([]string{}, h.groups...),
	}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		w:      h.w,
		color:  h.color,
		isTTY:  h.isTTY,
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: append(append([]string{}, h.groups...), name),
	}
}

func (h *Handler) format(r slog.Record) (string, bool) {
	attrs := collectAttrs(r, h.attrs)
	switch r.Message {
	case "progress":
		return h.formatProgress(attrs), !attrsBool(attrs, "final")
	case "archiving latest release":
		return h.tag("Fetch", cyan) + " latest release " + h.em(strAttr(attrs, "version"), bold), false
	case "archiving versions":
		return h.tag("Fetch", cyan) + " versions " + h.em(strings.Join(strSliceAttr(attrs, "versions"), ", "), bold), false
	case "archiving filtered versions":
		return fmt.Sprintf("%s %s versions  |  releases %d  |  snapshots %d  |  prereleases %d  |  rc %d  |  alpha %d  |  beta %d",
			h.tag("Plan", cyan),
			h.em(fmt.Sprintf("%d", intAttr(attrs, "total")), bold),
			intAttr(attrs, "releases"),
			intAttr(attrs, "snapshots"),
			intAttr(attrs, "prereleases"),
			intAttr(attrs, "rc"),
			intAttr(attrs, "alpha"),
			intAttr(attrs, "beta"),
		), false
	case "archive plan":
		return fmt.Sprintf("%s %s to download  |  %d total  |  %d skipped  |  %d workers  |  agent %s",
			h.tag("Plan", cyan),
			h.em(fmt.Sprintf("%d", intAttr(attrs, "pending")), bold),
			intAttr(attrs, "total"),
			intAttr(attrs, "skipped"),
			intAttr(attrs, "workers"),
			strAttr(attrs, "agent"),
		), false
	case "claimed version":
		return h.paint(dim, fmt.Sprintf("  worker %d claimed %s", intAttr(attrs, "worker"), strAttr(attrs, "version"))), false
	case "archive complete":
		return h.tag("Done", green) + " archived " + h.em(fmt.Sprintf("%d", intAttr(attrs, "versions")), bold) + " version(s)", false
	case "archive up to date":
		return h.tag("Done", green) + " archive up to date (" + h.em(fmt.Sprintf("%d", intAttr(attrs, "versions")), bold) + " versions)", false
	case "fabric archiving version":
		return h.tag("Fetch", cyan) + " fabric for " + h.em(strAttr(attrs, "version"), bold), false
	case "fabric archiving versions":
		return h.tag("Fetch", cyan) + " fabric for " + h.em(strings.Join(strSliceAttr(attrs, "versions"), ", "), bold), false
	case "fabric archive plan":
		line := fmt.Sprintf("%s %s minecraft  |  %s loaders",
			h.tag("Plan", cyan),
			h.em(fmt.Sprintf("%d", intAttr(attrs, "minecraft")), bold),
			h.em(fmt.Sprintf("%d", intAttr(attrs, "loaders")), bold),
		)
		if attrsBool(attrs, "all_loaders") {
			line += "  |  all loaders"
		}
		line += fmt.Sprintf("  |  %d workers", intAttr(attrs, "workers"))
		return line, false
	case "fabric skip":
		return h.paint(dim, fmt.Sprintf("  skip %s (%s)", strAttr(attrs, "minecraft"), strAttr(attrs, "reason"))), false
	case "fabric archive complete":
		return h.tag("Done", green) + " fabric archived " +
			h.em(fmt.Sprintf("%d", intAttr(attrs, "minecraft")), bold) + " minecraft  |  " +
			h.em(fmt.Sprintf("%d", intAttr(attrs, "loaders")), bold) + " loader(s)", false
	case "manifest written":
		return h.tag("Manifest", green) + " written (" + h.em(fmt.Sprintf("%d", intAttr(attrs, "versions")), bold) + " versions) -> " + strAttr(attrs, "path"), false
	case "starting server":
		return fmt.Sprintf("%s listening on %s  ->  %s  (data: %s)",
			h.tag("Server", green),
			h.em(strAttr(attrs, "listen"), bold),
			strAttr(attrs, "public"),
			strAttr(attrs, "data"),
		), false
	case "listening":
		return fmt.Sprintf("%s ready on %s  ->  %s",
			h.tag("Server", green),
			h.em(strAttr(attrs, "addr"), bold),
			strAttr(attrs, "public"),
		), false
	case "shutting down":
		return h.paint(yellow, "Shutting down..."), false
	case "export started":
		return h.tag("Export", cyan) + " " + strAttr(attrs, "source") + " -> " + h.em(strAttr(attrs, "dest"), bold), false
	case "export complete":
		return h.tag("Export", green) + " wrote " + h.em(strAttr(attrs, "dest"), bold) +
			fmt.Sprintf(" (%d files, %s)", intAttr(attrs, "files"), formatBytes(int64Attr(attrs, "bytes"))), false
	case "import started":
		return h.tag("Import", cyan) + " " + strAttr(attrs, "source") + " -> " + h.em(strAttr(attrs, "dest"), bold), false
	case "import complete":
		return h.tag("Import", green) + " restored " + h.em(strAttr(attrs, "dest"), bold) +
			fmt.Sprintf(" (%d files, %s)", intAttr(attrs, "files"), formatBytes(int64Attr(attrs, "bytes"))), false
	case "archive failed", "archive", "archive-fabric", "serve", "config", "data dir", "export", "import":
		return h.tag("Error", red) + " " + strAttr(attrs, "err"), false
	case "unknown command":
		return h.tag("Error", red) + " unknown command " + h.em(strAttr(attrs, "cmd"), bold) + " (" + strAttr(attrs, "hint") + ")", false
	default:
		return h.formatDefault(r, attrs), false
	}
}

func (h *Handler) formatProgress(attrs map[string]any) string {
	filesDone := int64Attr(attrs, "files_done")
	filesTotal := int64Attr(attrs, "files_total")
	filePct := floatAttr(attrs, "files_pct")
	bytesDone := int64Attr(attrs, "bytes")
	speed := floatAttr(attrs, "speed_bps")
	versionsDone := intAttr(attrs, "versions_done")
	versionsTotal := intAttr(attrs, "versions_total")

	line := fmt.Sprintf("%s %s/%s files (%.1f%%)  |  %s  |  %s/s",
		h.tag("Progress", green),
		h.em(fmt.Sprintf("%d", filesDone), bold),
		fmt.Sprintf("%d", filesTotal),
		filePct,
		formatBytes(bytesDone),
		formatSpeed(speed),
	)

	if working := strSliceAttr(attrs, "working"); len(working) > 0 {
		line += "  |  " + h.em(strings.Join(working, ", "), bold)
	}
	if versionsTotal > 1 {
		line += fmt.Sprintf("  |  versions %d/%d", versionsDone, versionsTotal)
	}
	if attrsBool(attrs, "final") {
		line += "  " + h.tag("complete", green)
	}
	return line
}

func (h *Handler) formatDefault(r slog.Record, attrs map[string]any) string {
	level := strings.ToLower(r.Level.String())
	parts := []string{h.paint(level, dim), r.Message}
	for k, v := range attrs {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}

func (h *Handler) tag(label, color string) string {
	return h.paint(color, label)
}

func (h *Handler) em(text, color string) string {
	return h.paint(color, text)
}

func (h *Handler) paint(color, text string) string {
	if !h.color {
		return text
	}
	return color + text + reset
}

func collectAttrs(r slog.Record, pre []slog.Attr) map[string]any {
	m := make(map[string]any, len(pre)+8)
	for _, a := range pre {
		m[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func strAttr(attrs map[string]any, key string) string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func intAttr(attrs map[string]any, key string) int {
	return int(int64Attr(attrs, key))
}

func int64Attr(attrs map[string]any, key string) int64 {
	v, ok := attrs[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func floatAttr(attrs map[string]any, key string) float64 {
	v, ok := attrs[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float32:
		return float64(n)
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func attrsBool(attrs map[string]any, key string) bool {
	v, ok := attrs[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func strSliceAttr(attrs map[string]any, key string) []string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div := int64(unit)
	exp := 0
	for n >= div && exp < 4 {
		if n < div*unit {
			break
		}
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := []string{"KB", "MB", "GB", "TB"}[exp]
	return fmt.Sprintf("%.1f %s", value, suffix)
}

func formatSpeed(bps float64) string {
	if bps <= 0 {
		return "0 B"
	}
	return formatBytes(int64(bps))
}
