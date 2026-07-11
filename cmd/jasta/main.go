/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package main

import (
	"go.osspkg.com/goppy/v3"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/api"
	"go.arwos.org/jasta/internal/pkg/grab"
	"go.arwos.org/jasta/internal/pkg/nginx"
)

var Version = "v0.0.0-dev"

func main() {
	app := goppy.New("jasta", Version, "Gateway for Static Sites")
	app.Plugins(
		web.WithServer(),
	)
	app.Plugins(api.Plugins)
	app.Command(nginx.Command)
	app.Command(grab.Command)
	app.Run()
}
