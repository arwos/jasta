/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package jasta

import (
	"fmt"
	"path/filepath"
	"strings"

	"go.osspkg.com/goppy/v2/plugins"
	"go.osspkg.com/ioutils/codec"
	"go.osspkg.com/ioutils/fs"
)

var Plugins = plugins.Inject(
	plugins.Plugin{
		Config: &Config{},
		Inject: WebsiteConfigDecode,
	},
	plugins.Plugin{
		Inject: New,
	},
)

func WebsiteConfigDecode(c *Config) (WebsiteConfigs, error) {
	result := make([]*WebsiteConfig, 0, 10)
	files, err := fs.SearchFilesByExt(c.Websites, ".yaml")
	if err != nil {
		return nil, fmt.Errorf("detect websites configs: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no configs for websites")
	}
	for _, filename := range files {
		wc := &WebsiteConfig{}
		if err = codec.FileEncoder(filename).Decode(wc); err != nil {
			return nil, fmt.Errorf("invalid website config [%s]: %w", filename, err)
		}
		if len(wc.Root) > 0 && wc.Root[0] == '.' {
			filenameFull, err0 := filepath.Abs(filename)
			if err0 != nil {
				return nil, fmt.Errorf("validate root path for [%s]: %w", filename, err0)
			}
			wc.Root = filepath.Dir(filenameFull) + "/" + strings.TrimLeft(wc.Root, "./")
		}
		if err = wc.Validate(); err != nil {
			return nil, err
		}
		result = append(result, wc)
	}
	return result, nil
}
