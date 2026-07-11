package config

import (
	"path/filepath"
	"sort"

	"go.osspkg.com/ioutils/codec"
	"go.osspkg.com/ioutils/fs"
	"go.osspkg.com/logx"
)

func SearchApplicationConfigs(dir string) []any {
	files, err := fs.SearchFilesByExt(dir, ".yaml")
	if err != nil {
		logx.Error("failed to detect websites configs", "err", err)
		return nil
	}
	if len(files) == 0 {
		logx.Error("no configs for websites", "err", err)
		return nil
	}

	sort.Strings(files)

	configs := make([]any, 0, len(files))
	for _, file := range files {

		bc := Base{}
		if err = codec.FileEncoder(file).Decode(&bc); err != nil {
			logx.Error("failed to decode website config",
				"file", file, "err", err)
			continue
		}

		ac, err := GetApplicationConfigByType(bc.ApplicationType)
		if err != nil {
			logx.Error("failed to get application config by type",
				"file", file, "type", bc.ApplicationType, "err", err)
			continue
		}

		if err = codec.FileEncoder(file).Decode(ac); err != nil {
			logx.Error("failed to decode application config",
				"file", file, "err", err)
			continue
		}

		if vl, ok := ac.(Validater); ok {
			if err = vl.Validate(); err != nil {
				logx.Error("failed to validate application config",
					"file", file, "err", err)
				continue
			}
		}

		if rs, ok := ac.(Rooter); ok {
			root := rs.GetRoot()

			if len(root) > 0 && root[0] == '.' {
				root, err = filepath.Abs(filepath.Dir(file) + "/" + root[1:])
				if err != nil {
					logx.Error("failed to resolve absolute root path",
						"file", file, "err", err)
					continue
				}

				rs.SetRoot(root)
			}
		}

		configs = append(configs, ac)
	}

	return configs
}
