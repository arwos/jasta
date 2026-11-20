package archive

import (
	"fmt"
	"path"
	"strings"
)

func normalizeArchivePath(name string, directory bool) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("invalid archive path %q", name)
	}
	if strings.HasPrefix(name, "/") || (len(name) >= 2 && isASCIIAlpha(name[0]) && name[1] == ':') {
		return "", fmt.Errorf("absolute archive path %q is not allowed", name)
	}
	if directory {
		name = strings.TrimSuffix(name, "/")
	}
	parts := strings.Split(name, "/")
	clean := make([]string, 0, len(parts))
	for index, part := range parts {
		switch part {
		case "":
			return "", fmt.Errorf("ambiguous archive path %q", name)
		case ".":
			if index == 0 {
				continue
			}
			return "", fmt.Errorf("ambiguous archive path %q", name)
		case "..":
			return "", fmt.Errorf("path traversal in archive path %q", name)
		default:
			clean = append(clean, part)
		}
	}
	if len(clean) == 0 {
		if directory {
			return "", nil
		}
		return "", fmt.Errorf("archive file path %q names the staging root", name)
	}
	return path.Join(clean...), nil
}

func normalizeLinkTarget(name, target string) (string, error) {
	if target == "" || strings.ContainsRune(target, 0) || strings.ContainsRune(target, '\\') {
		return "", fmt.Errorf("invalid link target %q for %q", target, name)
	}
	if strings.HasPrefix(target, "/") || (len(target) >= 2 && isASCIIAlpha(target[0]) && target[1] == ':') {
		return "", fmt.Errorf("absolute link target %q for %q is not allowed", target, name)
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", fmt.Errorf("link target %q for %q escapes staging", target, name)
	}
	return target, nil
}

func isASCIIAlpha(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}
