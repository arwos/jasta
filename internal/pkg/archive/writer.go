package archive

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

type pendingLink struct {
	name   string
	target string
	kind   pathKind
}

type extractor struct {
	root     *os.Root
	limits   Limits
	budget   byteBudget
	paths    pathIndex
	links    []pendingLink
	entries  int
	rootSeen bool
}

func newExtractor(root *os.Root, limits Limits) *extractor {
	return &extractor{
		root:   root,
		limits: limits,
		budget: byteBudget{limit: limits.MaxUnpacked},
		paths:  make(pathIndex),
	}
}

func (x *extractor) countEntry() error {
	if x.entries >= x.limits.MaxEntries {
		return ErrTooManyEntries
	}
	x.entries++
	return nil
}

func (x *extractor) addDirectory(name string) error {
	if err := x.countEntry(); err != nil {
		return err
	}
	name, err := normalizeArchivePath(name, true)
	if err != nil {
		return err
	}
	if name == "" {
		if x.rootSeen {
			return fmt.Errorf("duplicate archive root directory entry")
		}
		x.rootSeen = true
		return nil
	}
	if err = x.paths.add(name, kindDirectory); err != nil {
		return err
	}
	if err = x.root.MkdirAll(name, 0o755); err != nil {
		return fmt.Errorf("create archive directory %q: %w", name, err)
	}
	return nil
}

func (x *extractor) addFile(name string, source io.Reader) error {
	if err := x.countEntry(); err != nil {
		return err
	}
	name, err := normalizeArchivePath(name, false)
	if err != nil {
		return err
	}
	if err = x.paths.add(name, kindRegular); err != nil {
		return err
	}
	if err = x.makeParent(name); err != nil {
		return err
	}
	file, err := x.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create extracted file %q: %w", name, err)
	}
	_, copyErr := x.budget.copy(file, source)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("extract file %q: %w", name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close extracted file %q: %w", name, closeErr)
	}
	return nil
}

func (x *extractor) addSymlink(name, target string, countTarget bool) error {
	if err := x.countEntry(); err != nil {
		return err
	}
	name, err := normalizeArchivePath(name, false)
	if err != nil {
		return err
	}
	target, err = normalizeLinkTarget(name, target)
	if err != nil {
		return err
	}
	if len(target) > 4096 {
		return fmt.Errorf("symlink target for %q is too long", name)
	}
	if countTarget {
		if err = x.budget.add(int64(len(target))); err != nil {
			return fmt.Errorf("symlink target for %q: %w", name, err)
		}
	}
	if err = x.paths.add(name, kindSymlink); err != nil {
		return err
	}
	if err = x.makeParent(name); err != nil {
		return err
	}
	x.links = append(x.links, pendingLink{name: name, target: target, kind: kindSymlink})
	return nil
}

func (x *extractor) addHardlink(name, target string) error {
	if err := x.countEntry(); err != nil {
		return err
	}
	name, err := normalizeArchivePath(name, false)
	if err != nil {
		return err
	}
	target, err = normalizeArchivePath(target, false)
	if err != nil {
		return fmt.Errorf("invalid hardlink target for %q: %w", name, err)
	}
	if err = x.paths.add(name, kindHardlink); err != nil {
		return err
	}
	if err = x.makeParent(name); err != nil {
		return err
	}
	x.links = append(x.links, pendingLink{name: name, target: target, kind: kindHardlink})
	return nil
}

func (x *extractor) makeParent(name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	if err := x.root.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", name, err)
	}
	return nil
}

func (x *extractor) finish() error {
	if x.entries == 0 {
		return fmt.Errorf("archive is empty")
	}
	for _, link := range x.links {
		if link.kind == kindSymlink {
			if err := x.root.Symlink(link.target, link.name); err != nil {
				return fmt.Errorf("create symlink %q: %w", link.name, err)
			}
			continue
		}
		targetNode, ok := x.paths[link.target]
		if !ok || targetNode.kind != kindRegular {
			return fmt.Errorf("hardlink %q target %q is not an extracted regular file", link.name, link.target)
		}
		if err := x.root.Link(link.target, link.name); err != nil {
			return fmt.Errorf("create hardlink %q: %w", link.name, err)
		}
	}
	return nil
}

func (x *extractor) readSymlink(name string, source io.Reader) error {
	const maxLinkTarget = 4096
	data, err := io.ReadAll(io.LimitReader(source, maxLinkTarget+1))
	if err != nil {
		return fmt.Errorf("read symlink entry %q: %w", name, err)
	}
	if len(data) > maxLinkTarget {
		return fmt.Errorf("symlink target for %q is too long", name)
	}
	if err = x.budget.add(int64(len(data))); err != nil {
		return fmt.Errorf("symlink target for %q: %w", name, err)
	}
	target := string(data)
	if strings.ContainsRune(target, 0) {
		return fmt.Errorf("symlink target for %q contains NUL", name)
	}
	return x.addSymlink(name, target, false)
}
