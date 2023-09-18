package utils

import (
	"io/fs"
	"path/filepath"
)

func AllFileByExt(dir, ext string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(info.Name()) != ext {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}
