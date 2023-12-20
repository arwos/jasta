/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package command

import (
	"github.com/osspkg/jasta/internal/spiderweb"
	"go.osspkg.com/goppy/console"
)

func PreRenderStaticWebsites() {
	err := spiderweb.New().Run()
	console.FatalIfErr(err, "grab web")
	console.Infof("Done")
}
