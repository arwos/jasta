package admin

import (
	"fmt"

	"go.arwos.org/jasta/internal/pkg/archive"
)

const (
	defaultMasterKeyPath = "/etc/jasta/master.key"
	defaultReleasesRoot  = "/var/lib/jasta/releases"
	defaultReloadSocket  = "/run/jasta/web-reload.sock"
)

type (
	GroupConfig struct {
		Admin Config `yaml:"admin"`
	}
	Config struct {
		Tag           string         `yaml:"tag"`
		Archive       archive.Limits `yaml:"archive"`
		MasterKeyPath string         `yaml:"master_key_path"`
		ReleasesRoot  string         `yaml:"releases_root"`
		ReloadSocket  string         `yaml:"reload_socket"`
	}
)

func (c *GroupConfig) Default() {
	if len(c.Admin.Tag) == 0 {
		c.Admin.Tag = "admin"
	}
	c.Admin.Archive.SetDefaults()
	if c.Admin.MasterKeyPath == "" {
		c.Admin.MasterKeyPath = defaultMasterKeyPath
	}
	if c.Admin.ReleasesRoot == "" {
		c.Admin.ReleasesRoot = defaultReleasesRoot
	}
	if c.Admin.ReloadSocket == "" {
		c.Admin.ReloadSocket = defaultReloadSocket
	}
}

func (c *GroupConfig) Validate() error {
	if len(c.Admin.Tag) == 0 {
		return fmt.Errorf("admin tag is required")
	}
	if c.Admin.MasterKeyPath == "" || c.Admin.ReleasesRoot == "" || c.Admin.ReloadSocket == "" {
		return fmt.Errorf("admin master key, releases root, and reload socket are required")
	}
	return c.Admin.Archive.Validate()
}
