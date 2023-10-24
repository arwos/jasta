/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package main

import (
	"github.com/osspkg/jasta/internal/command"
	"github.com/osspkg/jasta/internal/jasta"
	"go.osspkg.com/goppy"
	"go.osspkg.com/goppy/plugins/web"
)

func main() {
	app := goppy.New()
	app.Plugins(
		web.WithHTTP(),
	)
	app.Plugins(
		jasta.Plugins...,
	)
	app.Command("nginx", command.InstallNginxConfig)
	app.Command("prerender", command.PreRenderStaticWebsites)
	app.Run()
}
