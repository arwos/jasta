/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package app

import (
	"fmt"

	"go.osspkg.com/ioutils/fs"
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
	if !fs.FileExist(c.Websites) {
		return fmt.Errorf("websites folder path is not exist")
	}
	return nil
}

// --------------------------------------------------------------------------------------

const (
	TypeSPA = "spa"
	TypeMPA = "mpa"
	TypeMD  = "md"
)

type (
	WebsiteConfigs []*WebsiteConfig

	WebsiteConfig struct {
		Type         string       `yaml:"type"`
		Domains      []string     `yaml:"domains"`
		RootFolder   string       `yaml:"root_folder"`
		AssetsFolder string       `yaml:"assets_folder"`
		Page404File  string       `yaml:"page404_file"`
		Placeholders Placeholders `yaml:"placeholders,omitempty"`
		Markdown     MarkdownTmpl `yaml:"markdown,omitempty"`
	}

	Placeholders map[string]string
	MarkdownTmpl struct {
		Header string `yaml:"header"`
		Footer string `yaml:"footer"`
	}
)

func (c *WebsiteConfig) Validate() error {
	switch c.Type {
	case TypeSPA, TypeMPA:
	case TypeMD:
		if len(c.Markdown.Header) == 0 || !fs.FileExist(c.Markdown.Header) {
			return fmt.Errorf("markdown header is empty or does not exist")
		}
		if len(c.Markdown.Footer) == 0 || !fs.FileExist(c.Markdown.Footer) {
			return fmt.Errorf("markdown footer is empty or does not exist")
		}
	default:
		return fmt.Errorf("unknown type: %s (possible spa, mpa, md)", c.Type)
	}
	if len(c.Domains) == 0 {
		return fmt.Errorf("invalid domain")
	}
	if len(c.RootFolder) == 0 || !fs.FileExist(c.RootFolder) {
		return fmt.Errorf("invalid root folder")
	}
	if len(c.AssetsFolder) == 0 {
		return fmt.Errorf("invalid assets folder")
	}
	if len(c.Page404File) == 0 {
		return fmt.Errorf("invalid page 404 file")
	}
	return nil
}
