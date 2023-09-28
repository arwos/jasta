/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package jasta

import (
	"fmt"

	"github.com/osspkg/go-sdk/iofile"
)

type Config struct {
	Websites string `yaml:"websites"`
}

func (c *Config) Default() {
	if len(c.Websites) == 0 {
		c.Websites = "/etc/jasta/websites"
	}
}

func (c *Config) Validate() error {
	if len(c.Websites) == 0 {
		return fmt.Errorf("websites folder path is not defined")
	}
	if !iofile.Exist(c.Websites) {
		return fmt.Errorf("websites folder path is not exist")
	}
	return nil
}

type (
	WebsiteConfigs []*WebsiteConfig

	WebsiteConfig struct {
		Single       bool         `yaml:"single"`
		Domains      []string     `yaml:"domains"`
		Root         string       `yaml:"root"`
		AssetsFolder string       `yaml:"assets_folder"`
		Page404      string       `yaml:"page404"`
		Placeholders Placeholders `yaml:"placeholders,omitempty"`
	}

	Placeholders map[string]string
)

func (c *WebsiteConfig) Validate() error {
	if len(c.Domains) == 0 {
		return fmt.Errorf("invalid domain")
	}
	if len(c.Root) == 0 || !iofile.Exist(c.Root) {
		return fmt.Errorf("invalid root folder")
	}
	if len(c.AssetsFolder) == 0 {
		return fmt.Errorf("invalid assets folder")
	}
	if len(c.Page404) == 0 {
		return fmt.Errorf("invalid page 404 file")
	}
	return nil
}
