/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package command

import (
	"os"

	"go.osspkg.com/console"
)

const nginxConfigTemplate = `
server {
	listen 80 default_server;
	listen [::]:80 default_server;

	server_name _;

	location / {
		proxy_pass http://127.0.0.1:15432;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Real-IP $remote_addr;
	}
}
`

func InstallNginxConfig() {
	err := os.WriteFile("/etc/nginx/sites-available/default", []byte(nginxConfigTemplate), 0744)
	console.FatalIfErr(err, "write nginx config")
	console.Infof("Done")
}
