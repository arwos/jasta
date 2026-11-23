package archive

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Format string

const (
	FormatZIP   Format = "zip"
	FormatTARGZ Format = "tar.gz"
)

var (
	ErrArchiveTooLarge  = errors.New("archive exceeds configured input limit")
	ErrUnpackedTooLarge = errors.New("archive exceeds configured unpacked limit")
	ErrTooManyEntries   = errors.New("archive exceeds configured entry count")
)

// Extract writes an archive into a new private directory below stagingParent.
// The caller owns the returned directory and must retain it as a release or
// remove it after a failed deployment.
func Extract(input io.Reader, format Format, stagingParent string, limits Limits) (_ string, err error) {
	if input == nil {
		return "", errors.New("archive input is required")
	}
	if stagingParent == "" {
		return "", errors.New("staging parent is required")
	}
	limits, err = limits.withDefaults()
	if err != nil {
		return "", err
	}
	maxInt64 := int64(^uint64(0) >> 1)
	if limits.MaxArchiveBytes == maxInt64 || limits.MaxUnpacked == maxInt64 {
		return "", errors.New("archive limits are too large")
	}
	parent, err := filepath.Abs(stagingParent)
	if err != nil {
		return "", fmt.Errorf("resolve staging parent: %w", err)
	}
	if err = os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create staging parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "jasta-archive-*")
	if err != nil {
		return "", fmt.Errorf("create archive staging directory: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(stage)
		}
	}()
	root, err := os.OpenRoot(stage)
	if err != nil {
		return "", fmt.Errorf("open archive staging directory: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close archive staging directory: %w", closeErr)
		}
	}()

	switch format {
	case FormatZIP:
		err = extractZIP(input, root, stage, limits)
	case FormatTARGZ:
		err = extractTARGZ(input, root, limits)
	default:
		err = fmt.Errorf("unsupported archive format %q", format)
	}
	if err != nil {
		return "", err
	}
	return stage, nil
}

type byteBudget struct {
	limit int64
	used  int64
}

func (b *byteBudget) add(n int64) error {
	if n < 0 || n > b.limit-b.used {
		return ErrUnpackedTooLarge
	}
	b.used += n
	return nil
}

func (b *byteBudget) copy(dst io.Writer, src io.Reader) (int64, error) {
	remaining := b.limit - b.used
	if remaining < math.MaxInt64 {
		limited := &io.LimitedReader{R: src, N: remaining + 1}
		n, err := io.Copy(dst, limited)
		if n > remaining {
			return n, ErrUnpackedTooLarge
		}
		b.used += n
		return n, err
	}
	n, err := io.Copy(dst, src)
	if err != nil {
		return n, err
	}
	if err = b.add(n); err != nil {
		return n, err
	}
	return n, nil
}

type pathKind uint8

const (
	kindDirectory pathKind = iota + 1
	kindRegular
	kindSymlink
	kindHardlink
)

type pathNode struct {
	kind     pathKind
	explicit bool
}

type pathIndex map[string]pathNode

func (i pathIndex) add(name string, kind pathKind) error {
	parts := splitPath(name)
	for n := 1; n < len(parts); n++ {
		parent := path.Join(parts[:n]...)
		node, exists := i[parent]
		if exists && node.kind != kindDirectory {
			return fmt.Errorf("archive path %q has non-directory parent %q", name, parent)
		}
		if !exists {
			i[parent] = pathNode{kind: kindDirectory}
		}
	}
	if previous, exists := i[name]; exists {
		if kind == kindDirectory && previous.kind == kindDirectory && !previous.explicit {
			i[name] = pathNode{kind: kindDirectory, explicit: true}
			return nil
		}
		return fmt.Errorf("duplicate or conflicting archive path %q", name)
	}
	i[name] = pathNode{kind: kind, explicit: true}
	return nil
}

func splitPath(name string) []string {
	return strings.Split(name, "/")
}
