/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package jasta

import (
	"fmt"

	"github.com/osspkg/go-sdk/app"
	"github.com/osspkg/goppy/plugins"
	"github.com/osspkg/jasta/internal/utils"
)

var Plugins = plugins.Plugins{}.Inject(
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
	files, err := utils.AllFileByExt(c.Websites, ".yaml")
	if err != nil {
		return nil, fmt.Errorf("detect websites configs: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no configs for websites")
	}
	for _, filename := range files {
		wc := &WebsiteConfig{}
		if err = app.Sources(filename).Decode(wc); err != nil {
			return nil, fmt.Errorf("invalid website config [%s]: %w", filename, err)
		}
		if err = wc.Validate(); err != nil {
			return nil, err
		}
		result = append(result, wc)
	}
	return result, nil
}
