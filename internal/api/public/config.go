package public

import (
	"fmt"

	"go.osspkg.com/ioutils/fs"
)

type (
	GroupConfig struct {
		Public Config `yaml:"public"`
	}
	Config struct {
		Tag          string `yaml:"tag"`
		WebSites     string `yaml:"websites"`
		Throttling   int64  `yaml:"throttling"`
		ReloadSocket string `yaml:"reload_socket"`
	}
)

func (c *GroupConfig) Default() {
	c.Public.WebSites = "/etc/jasta/websites"
	c.Public.Tag = "main"
	c.Public.Throttling = 300
	c.Public.ReloadSocket = "/run/jasta/web-reload.sock"
}

func (c *GroupConfig) Validate() error {
	if len(c.Public.Tag) == 0 {
		return fmt.Errorf("public tag is required")
	}
	if len(c.Public.WebSites) == 0 {
		return fmt.Errorf("websites folder path is not defined")
	}
	if !fs.FileExist(c.Public.WebSites) {
		return fmt.Errorf("websites folder path is not exist")
	}
	if len(c.Public.ReloadSocket) == 0 {
		return fmt.Errorf("reload socket path is required")
	}
	return nil
}
