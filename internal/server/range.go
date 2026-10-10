package server

import (
	"os"
)

var clenSmall [4096]string

func init() {
	for i := range clenSmall {
		clenSmall[i] = formatIntRaw(i)
	}
}

func formatIntRaw(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func parseRange(header string, size int) (start, end int, ok bool) {
	if len(header) < 7 || header[0] != 'b' || header[:6] != "bytes=" {
		return 0, 0, false
	}
	spec := header[6:]
	if i := indexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}
	dash := indexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	left := spec[:dash]
	right := spec[dash+1:]

	if len(left) == 0 {
		suffix, err := parseUint(right)
		if err != nil || suffix == 0 {
			return 0, 0, false
		}
		start = max(size-int(suffix), 0)
		return start, size - 1, true
	}

	start, err := parseUint(left)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	if len(right) == 0 {
		return start, size - 1, true
	}
	end, err = parseUint(right)
	if err != nil || end < start || end >= size {
		return 0, 0, false
	}
	return start, end, true
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func parseUint(s string) (int, error) {
	if len(s) == 0 {
		return 0, os.ErrInvalid
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func formatInt(n int) string {
	if n >= 0 && n < len(clenSmall) {
		return clenSmall[n]
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func contentLengthHeader(n int) string {
	return formatInt(n)
}

func appendInt(dst []byte, n int) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	if n < 0 {
		dst = append(dst, '-')
		n = -n
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

func appendRangeHeader(dst []byte, start, end, total int) []byte {
	dst = append(dst, "bytes "...)
	dst = appendInt(dst, start)
	dst = append(dst, '-')
	dst = appendInt(dst, end)
	dst = append(dst, '/')
	dst = appendInt(dst, total)
	return dst
}
