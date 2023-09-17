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

type WebsiteConfig struct {
	Domain       string       `yaml:"domain"`
	IndexFile    string       `yaml:"index_file"`
	SpecPages    []SpecPage   `yaml:"spec_pages"`
	Placeholders Placeholders `yaml:"placeholders,omitempty"`
}

type SpecPage struct {
	HttpCode int    `yaml:"http_code"`
	PageFile string `yaml:"page_file"`
}

type Placeholders map[string]string
