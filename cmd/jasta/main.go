/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package main

import (
	"os"
	"strings"

	"go.osspkg.com/goppy/v3"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/api"
	"go.arwos.org/jasta/internal/api/admin"
	"go.arwos.org/jasta/internal/pkg/grab"
	"go.arwos.org/jasta/internal/pkg/nginx"
)

var Version = "v0.0.0-dev"

func main() {
	role := serviceRole(os.Args)
	if len(os.Args) > 1 && (os.Args[1] == "web" || os.Args[1] == "admin") {
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	ensureRoleConfig(role)

	app := goppy.New("jasta", Version, "Gateway for Static Sites")
	app.Plugins(
		web.WithServer(),
	)
	if role == "admin" {
		app.Plugins(api.AdminPlugins)
	} else {
		app.Plugins(api.PublicPlugins)
	}
	app.Command(nginx.Command)
	app.Command(grab.Command)
	if role == "admin" {
		app.Command(admin.ActivateCommand)
		app.Command(admin.HistoryCommand)
		app.Command(admin.RollbackCommand)
		app.Command(admin.ReloadCommand)
		app.Command(admin.ImportConfigsCommand)
		app.Command(admin.OwnerBootstrapCommand)
		app.Command(admin.OwnerResetCommand)
	}
	app.Run()
}

func serviceRole(args []string) string {
	if len(args) > 1 && args[1] == "admin" {
		return "admin"
	}
	return "web"
}

func ensureRoleConfig(role string) {
	for _, arg := range os.Args[1:] {
		if arg == "--config" || strings.HasPrefix(arg, "--config=") {
			return
		}
	}

	configPath := "/etc/jasta/config.yaml"
	if role == "admin" {
		configPath = "/etc/jasta/admin.yaml"
	}
	os.Args = append(os.Args, "--config="+configPath)
}
