package spiderweb

import (
	"go.osspkg.com/ioutils/fs"
	"go.osspkg.com/ioutils/shell"
)

func setupShell() (shell.TShell, error) {
	sh := shell.New()
	sh.SetDir(fs.CurrentDir())
	sh.UseOSEnv(true)
	if err := sh.SetShell("/bin/bash", "x", "e", "c"); err != nil {
		return nil, err
	}
	return sh, nil
}
