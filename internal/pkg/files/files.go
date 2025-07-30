package files

import (
	"io/fs"
	"path/filepath"
	"strings"
)

func GetAll(dir string) ([]string, error) {
	files := make([]string, 0, 2)
	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		path = strings.TrimPrefix(path, dir)
		if path[0] != '/' {
			path = "/" + path
		}
		files = append(files, path)
		return nil
	})
	return files, err
}
