package admin

import (
	"fmt"
)

type (
	GroupConfig struct {
		Admin Config `yaml:"admin"`
	}
	Config struct {
		Tag string `yaml:"tag"`
	}
)

func (c *GroupConfig) Default() {
	if len(c.Admin.Tag) == 0 {
		c.Admin.Tag = "admin"
	}
}

func (c *GroupConfig) Validate() error {
	if len(c.Admin.Tag) == 0 {
		return fmt.Errorf("admin tag is required")
	}
	return nil
}
