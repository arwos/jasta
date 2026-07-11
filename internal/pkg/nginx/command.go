package nginx

import (
	"context"
	"os"

	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"
)

const template = `
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

func Command(_ context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("nginx", "Setup Nginx default config")
	setter.ExecFunc(func() {
		err := os.WriteFile("/etc/nginx/sites-available/default", []byte(template), 0744)
		console.FatalIfErr(err, "write nginx config")
		console.Infof("Done")
	})
}
