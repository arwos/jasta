package urlclean

import (
	"strings"

	"go.osspkg.com/ioutils/pool"
)

func Clean(path string) string {
	if path == "" || path == "/" {
		return ""
	}
	if path[0] == '/' {
		path = path[1:]
	}
	if path == "" {
		return ""
	}
	if !strings.Contains(path, "/") && !strings.Contains(path, ".") {
		return path
	}
	return cleanSlow(path)
}

var stringPool = pool.NewSlicePool[string](0, 1024)

func cleanSlow(path string) string {
	segments := stringPool.Get()
	defer stringPool.Put(segments)

	i := 0
	if len(path) > 0 && path[0] == '/' {
		i = 1
	}

	for i < len(path) {
		for i < len(path) && path[i] == '/' {
			i++
		}

		if i == len(path) {
			break
		}

		start := i
		for i < len(path) && path[i] != '/' {
			i++
		}

		segment := path[start:i]
		switch segment {
		case ".":
		case "..":
			if len(segments.B) > 0 {
				segments.B = segments.B[:len(segments.B)-1]
			}
		default:
			segments.B = append(segments.B, segment)
		}
	}

	var b strings.Builder
	b.Grow(len(path))
	for idx, seg := range segments.B {
		if idx > 0 {
			b.WriteByte('/')
		}
		b.WriteString(seg)
	}

	res := b.String()
	if res == "" {
		return ""
	}
	return res
}
