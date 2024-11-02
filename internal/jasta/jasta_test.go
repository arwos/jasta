package jasta

import (
	"testing"
)

func TestUnit_protect(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "Case1",
			path: "./..//....///index.html.",
			want: "/index.html",
		},
		{
			name: "Case2",
			path: "./..//....///index.html./",
			want: "/index.html/",
		},
		{
			name: "Case3",
			path: "./..//....///index.html.///",
			want: "/index.html/",
		},
		{
			name: "Case4",
			path: "./..//....///index.html.///.png/",
			want: "/index.html/png/",
		},
		{
			name: "Case5",
			path: "..../..//....///index.html.///.png/...",
			want: "/index.html/png/",
		},
		{
			name: "Case6",
			path: "/hello/asd.aaa/index.html",
			want: "/hello/asd.aaa/index.html",
		},
		{
			name: "Case7",
			path: "hello/asd.aaa/index.html",
			want: "hello/asd.aaa/index.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protect(tt.path); got != tt.want {
				t.Errorf("protect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkProtect(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		protect("..../..//....///index.html.///.png/...")
	}
}
