/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package spiderweb

const runChromium = `DISPLAY=:0 chromium  \
			--headless \
			--incognito \
			--process-per-site \
			--disable-extensions \
			--disable-component-extensions-with-background-pages \
			--disable-component-update \
			--disable-default-apps \
			--disable-dev-shm-usage \
			--disable-background-networking \
			--enable-features=NetworkService,NetworkServiceInProcess \
			--disable-breakpad \
			--disable-gpu \
			--disable-demo-mode \
			--disable-client-side-phishing-detection \
			--disable-browser-task-scheduler \
			--disable-sync-preferences \
			--disable-device-disabling \
			--disable-device-discovery-notifications \
			--disable-logging \
			--disable-backgrounding-occluded-windows \
			--disable-file-system \
			--disable-hang-monitor \
			--disable-composited-antialiasing \
			--disable-notifications \
			--mute-audio \
			--disable-audio-support-for-desktop-share \
			--no-default-browser-check \
			--no-service-autorun \
			--no-first-run \
			--no-experiments \
			--no-managed-user-acknowledgment-check \
			--no-network-profile-warning \
			--no-use-mus-in-renderer \
			--noerrdialogs \
			--non-material \
			--enable-zero-copy \
			--lang=en-US \
			--user-data-dir=%s \
			--dump-dom '%s'`
