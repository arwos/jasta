package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractZIPAndTARGZ(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format Format
		data   []byte
	}{
		{"zip", FormatZIP, makeZIP(t, map[string]string{"index.html": "hello", "assets/app.js": "export {};"})},
		{"tar.gz", FormatTARGZ, makeTARGZ(t, []tarEntry{{name: "index.html", kind: tar.TypeReg, body: "hello"}, {name: "assets/app.js", kind: tar.TypeReg, body: "export {};"}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			stage, err := Extract(bytes.NewReader(tc.data), tc.format, parent, Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 1 << 20, MaxEntries: 10})
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(stage)
			got, err := os.ReadFile(filepath.Join(stage, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "hello" {
				t.Fatalf("got %q", got)
			}
			if _, err = os.Stat(filepath.Join(stage, "assets/app.js")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExtractRejectsInputAndUnpackedLimits(t *testing.T) {
	data := makeZIP(t, map[string]string{"index.html": "12345"})
	for _, tc := range []struct {
		name   string
		limits Limits
		want   error
	}{
		{"compressed", Limits{MaxArchiveBytes: int64(len(data) - 1), MaxUnpacked: 100, MaxEntries: 10}, ErrArchiveTooLarge},
		{"unpacked", Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 4, MaxEntries: 10}, ErrUnpackedTooLarge},
		{"entries", Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 100, MaxEntries: 0}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := tc.limits
			if tc.name == "entries" {
				limits.MaxEntries = 1
			}
			parent := t.TempDir()
			input := data
			if tc.name == "entries" {
				input = makeZIP(t, map[string]string{"index.html": "x", "other": "y"})
			}
			_, err := Extract(bytes.NewReader(input), FormatZIP, parent, limits)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			entries, readErr := os.ReadDir(parent)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("failed extraction left staging data: %v", entries)
			}
		})
	}
}

func TestExtractTARGZLimits(t *testing.T) {
	data := makeTARGZ(t, []tarEntry{{name: "content.txt", kind: tar.TypeReg, body: "12345"}})
	for _, tc := range []struct {
		name   string
		limits Limits
		want   error
	}{
		{"compressed", Limits{MaxArchiveBytes: int64(len(data) - 1), MaxUnpacked: 100, MaxEntries: 10}, ErrArchiveTooLarge},
		{"unpacked", Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 4, MaxEntries: 10}, ErrUnpackedTooLarge},
		{"entries", Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 100, MaxEntries: 0}, ErrTooManyEntries},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			limits := tc.limits
			input := data
			if tc.name == "entries" {
				limits.MaxEntries = 1
				input = makeTARGZ(t, []tarEntry{{name: "a", kind: tar.TypeReg, body: "x"}, {name: "b", kind: tar.TypeReg, body: "y"}})
			}
			_, err := Extract(bytes.NewReader(input), FormatTARGZ, parent, limits)
			if err == nil || !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			entries, readErr := os.ReadDir(parent)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("failed extraction left staging data: %v", entries)
			}
		})
	}
}

func TestExtractRejectsTraversalAndEscapingLinks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "owned")
	for _, tc := range []struct {
		name   string
		format Format
		data   []byte
	}{
		{"zip-slip", FormatZIP, makeZIP(t, map[string]string{"../owned": "bad"})},
		{"absolute", FormatZIP, makeZIP(t, map[string]string{"/tmp/owned": "bad"})},
		{"tar-symlink", FormatTARGZ, makeTARGZ(t, []tarEntry{{name: "escape", kind: tar.TypeSymlink, link: "../../owned"}})},
		{"tar-hardlink", FormatTARGZ, makeTARGZ(t, []tarEntry{{name: "escape", kind: tar.TypeLink, link: "../../owned"}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			if _, err := Extract(bytes.NewReader(tc.data), tc.format, parent, Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 1 << 20, MaxEntries: 10}); err == nil {
				t.Fatal("expected rejection")
			}
			if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("outside path changed: %v", err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("failed extraction left stage: %v", entries)
			}
		})
	}
}

func TestExtractAllowsInternalSymlinkAfterFiles(t *testing.T) {
	data := makeTARGZ(t, []tarEntry{{name: "latest", kind: tar.TypeSymlink, link: "releases/v1/index.html"}, {name: "releases/v1/index.html", kind: tar.TypeReg, body: "ok"}})
	stage, err := Extract(bytes.NewReader(data), FormatTARGZ, t.TempDir(), Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 1 << 20, MaxEntries: 10})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	got, err := os.ReadFile(filepath.Join(stage, "latest"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractRejectsMalformedArchivesAndDuplicatePaths(t *testing.T) {
	duplicate := makeZIPEntries(t, []tarEntry{{name: "same.txt", kind: tar.TypeReg, body: "one"}, {name: "same.txt", kind: tar.TypeReg, body: "two"}})
	for _, tc := range []struct {
		name   string
		format Format
		data   []byte
	}{
		{"malformed zip", FormatZIP, []byte("not a ZIP")},
		{"malformed tar.gz", FormatTARGZ, []byte("not gzip")},
		{"duplicate zip path", FormatZIP, duplicate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			if _, err := Extract(bytes.NewReader(tc.data), tc.format, parent, Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 1 << 20, MaxEntries: 10}); err == nil {
				t.Fatal("expected rejection")
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("failed extraction left staging data: %v", entries)
			}
		})
	}
}

type tarEntry struct {
	name string
	kind byte
	body string
	link string
}

func makeZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func makeZIPEntries(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(w, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func makeTARGZ(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		h := &tar.Header{Name: entry.name, Typeflag: entry.kind, Linkname: entry.link, Mode: 0o644, Size: int64(len(entry.body))}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" {
			if _, err := io.WriteString(tw, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
