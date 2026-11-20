package admin

import (
	"fmt"

	"go.arwos.org/jasta/internal/pkg/archive"
)

type (
	GroupConfig struct {
		Admin Config `yaml:"admin"`
	}
	Config struct {
		Tag     string         `yaml:"tag"`
		Archive archive.Limits `yaml:"archive"`
	}
)

func (c *GroupConfig) Default() {
	if len(c.Admin.Tag) == 0 {
		c.Admin.Tag = "admin"
	}
	c.Admin.Archive.SetDefaults()
}

func (c *GroupConfig) Validate() error {
	if len(c.Admin.Tag) == 0 {
		return fmt.Errorf("admin tag is required")
	}
	return c.Admin.Archive.Validate()
}
