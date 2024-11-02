/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package main

import (
	"go.arwos.org/jasta/internal/command"
	"go.arwos.org/jasta/internal/jasta"
	"go.osspkg.com/goppy/v2"
	"go.osspkg.com/goppy/v2/web"
)

var Version = "v0.0.0-dev"

func main() {
	app := goppy.New("jasta", Version, "Gateway for static sites")
	app.Plugins(
		web.WithServer(),
	)
	app.Plugins(
		jasta.Plugins...,
	)
	app.Command("nginx", command.InstallNginxConfig)
	app.Command("prerender", command.PreRenderStaticWebsites)
	app.Run()
}
