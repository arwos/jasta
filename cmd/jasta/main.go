/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package main

import (
	"go.arwos.org/jasta/internal/app"
	"go.arwos.org/jasta/internal/command"
	"go.osspkg.com/goppy/v2"
	"go.osspkg.com/goppy/v2/web"
)

var Version = "v0.0.0-dev"

func main() {
	a := goppy.New("jasta", Version, "Gateway for static sites")
	a.Plugins(
		web.WithServer(),
	)
	a.Plugins(
		app.Plugins...,
	)
	a.Command("nginx", command.InstallNginxConfig)
	a.Command("prerender", command.PreRenderStaticWebsites)
	a.Run()
}
