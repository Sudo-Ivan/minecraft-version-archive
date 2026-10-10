package mojang

import "bytes"

type JavaInfo struct {
	Major     int
	Component string
}

func ExtractJavaVersion(data []byte) (JavaInfo, bool) {
	idx := bytes.Index(data, []byte(`"javaVersion"`))
	if idx < 0 {
		return JavaInfo{}, false
	}
	slice := data[idx:]
	if end := bytes.Index(slice, []byte{'}'}); end > 0 && end < 512 {
		slice = slice[:end+1]
	}
	major, ok := extractIntField(slice, "majorVersion")
	if !ok {
		return JavaInfo{}, false
	}
	component, _ := extractStringField(slice, "component")
	return JavaInfo{Major: major, Component: component}, true
}

func AppendJavaResponse(dst []byte, id string, info JavaInfo) []byte {
	dst = append(dst, `{"id":"`...)
	dst = append(dst, id...)
	dst = append(dst, `","major_version":`...)
	dst = strconvAppendInt(dst, info.Major)
	dst = append(dst, `,"component":"`...)
	dst = append(dst, info.Component...)
	dst = append(dst, `"}`...)
	return dst
}

func VersionIDCandidates(id string) []string {
	aliases := aliasCandidates(id)
	out := make([]string, 0, 1+len(aliases))
	out = append(out, id)
	out = append(out, aliases...)
	return out
}

func extractIntField(data []byte, key string) (int, bool) {
	needle := []byte(`"` + key + `":`)
	_, after, ok := bytes.Cut(data, needle)
	if !ok {
		return 0, false
	}
	return parseIntBytes(bytes.TrimSpace(after))
}

func extractStringField(data []byte, key string) (string, bool) {
	needle := []byte(`"` + key + `"`)
	_, after, ok := bytes.Cut(data, needle)
	if !ok {
		return "", false
	}
	rest := bytes.TrimSpace(after)
	if len(rest) == 0 || rest[0] != ':' {
		return "", false
	}
	rest = bytes.TrimSpace(rest[1:])
	if len(rest) == 0 || rest[0] != '"' {
		return "", false
	}
	rest = rest[1:]
	before, _, ok := bytes.Cut(rest, []byte{'"'})
	if !ok {
		return "", false
	}
	return string(before), true
}

func parseIntBytes(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	neg := false
	if b[0] == '-' {
		neg = true
		b = b[1:]
	}
	val := 0
	digits := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		val = val*10 + int(c-'0')
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	if neg {
		val = -val
	}
	return val, true
}

func strconvAppendInt(dst []byte, n int) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	neg := n < 0
	if neg {
		n = -n
		dst = append(dst, '-')
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return append(dst, buf[i:]...)
}
