/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package main

import (
	"github.com/osspkg/goppy"
	"github.com/osspkg/goppy/plugins/web"
	"github.com/osspkg/jasta/internal/jasta"
)

func main() {
	app := goppy.New()
	app.Plugins(
		web.WithHTTP(),
	)
	app.Plugins(
		jasta.Plugin,
	)
	app.Run()
}
