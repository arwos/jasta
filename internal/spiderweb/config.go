/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package spiderweb

const configName = ".jasta.yaml"

type Config struct {
	DevHost string `yaml:"dev_host"`
	OutDir  string `yaml:"out_dir"`
	Sitemap string `yaml:"sitemap"`
	Domain  string `yaml:"domain"`
}
