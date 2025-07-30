/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package command

import (
	"go.arwos.org/jasta/internal/pkg/spiderweb"
	"go.osspkg.com/console"
)

func PreRenderStaticWebsites() {
	err := spiderweb.New().Run()
	console.FatalIfErr(err, "grab web")
	console.Infof("Done")
}
