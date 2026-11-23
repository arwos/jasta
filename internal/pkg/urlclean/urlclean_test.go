package urlclean

import (
	"testing"
)

func TestUnit_Clean(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Базовые случаи
		{name: "empty", input: "", expected: ""},
		{name: "root", input: "/", expected: ""},
		{name: "double slash", input: "//", expected: ""},
		{name: "slash dot slash", input: "/./", expected: ""},
		{name: "slash dot dot slash", input: "/../", expected: ""},
		{name: "parent dir middle", input: "a/b/..", expected: "a"},
		{name: "parent dir with child", input: "a/b/../c", expected: "a/c"},
		{name: "dot slash prefix", input: "./a", expected: "a"},
		{name: "dot dot slash prefix", input: "../a", expected: "a"},
		{name: "multiple dot dot prefix", input: "../../a", expected: "a"},
		{name: "abs with dot dots beyond root", input: "/../../a", expected: "a"},
		{name: "complex relative", input: "a/b/c/../../d", expected: "a/d"},
		{name: "dot in middle", input: "a/./b", expected: "a/b"},
		{name: "multiple slashes", input: "a//b", expected: "a/b"},
		{name: "dot dot at end relative", input: "a/..", expected: ""},
		{name: "dot dot at end absolute", input: "/a/..", expected: ""},
		{name: "just dot dot", input: "..", expected: ""},
		{name: "just dot", input: ".", expected: ""},
		{name: "dot and dot dot relative", input: "a/./..", expected: ""},
		{name: "trailing slash after dot dot", input: "a/../", expected: ""},
		{name: "only dot slash dot", input: "./.", expected: ""},

		// Дополнительные граничные случаи, отражающие фактическое поведение
		{name: "double parent clears stack", input: "a/b/../..", expected: ""},
		{name: "abs double parent clears to root", input: "/a/b/../..", expected: ""},
		{name: "triple component with dot dot", input: "a/b/c/../..", expected: "a"},
		{name: "triple dot as literal name", input: "a/...", expected: "a/..."},
		{name: "abs triple dot", input: "/...", expected: "..."},
		{name: "many dots", input: ".....", expected: "....."},
		{name: "abs dot dot only", input: "/..", expected: ""},
		{name: "relative dot dots with extra slashes", input: "..//../", expected: ""},
		{name: "mixed dot dot and dot", input: ".././a", expected: "a"},
		{name: "consecutive slashes absolute", input: "///a//b//", expected: "a/b"},
		{name: "with file", input: "///a//b//index.html", expected: "a/b/index.html"},
		{name: "with file", input: "//index.html", expected: "index.html"},
		{name: "with file", input: "/index.html", expected: "index.html"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Clean(tt.input)
			if got != tt.expected {
				t.Errorf("Clean(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

/*
goos: linux
goarch: amd64
pkg: go.arwos.org/jasta/internal/pkg/urlclean
cpu: 12th Gen Intel(R) Core(TM) i9-12900KF
BenchmarkClean
BenchmarkClean//
BenchmarkClean//-24							38614875	        28.84 ns/op	       8 B/op	       1 allocs/op
BenchmarkClean//a/b/c
BenchmarkClean//a/b/c-24					327190935	         3.671 ns/op	       0 B/op	       0 allocs/op
BenchmarkClean/a/b/..
BenchmarkClean/a/b/..-24					28602848	        45.54 ns/op	       8 B/op	       1 allocs/op
BenchmarkClean///a//b/../c/./d
BenchmarkClean///a//b/../c/./d-24			20415862	        64.51 ns/op	      16 B/op	       1 allocs/op
BenchmarkClean/../../..
BenchmarkClean/../../..-24					33275788	        39.93 ns/op	       8 B/op	       1 allocs/op
BenchmarkClean/#00
BenchmarkClean/#00-24						1000000000	         0.7394 ns/op	       0 B/op	       0 allocs/op
*/
func BenchmarkClean(b *testing.B) {
	paths := []string{
		"/",
		"/a/b/c",
		"a/b/..",
		"//a//b/../c/./d",
		"../../..",
		"",
	}
	for _, p := range paths {
		b.Run(p, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = Clean(p)
			}
		})
	}
}
