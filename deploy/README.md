# Jasta on Alpine with Compose

The Compose stack builds an Alpine image with Podman and starts `jasta web` and `jasta admin` as separate, unprivileged processes. The admin container waits for the web reload socket before it starts.

`podman compose` uses an external Compose provider. Configure one that supports the Compose `depends_on.restart` option used here.

## Build and deploy

From the repository root, provide the SSH account, host or SSH alias, and absolute normalized deployment path on the server. `SVC_PATH` must not contain whitespace:

```sh
export SVC_USER=jasta
export SVC_HOST=server.example.com
export SVC_PATH=/opt/jasta
make -C deploy install
```

Use `install` for the first deployment; it fails if a Compose file already exists at `SVC_PATH/compose.yaml` or in the legacy `SVC_PATH/deploy/` layout. For later releases, run `make -C deploy update`; it requires an existing installation. Both commands build and transfer the image, load it in Podman, place `compose.yaml`, configs, and `websites/` directly in `SVC_PATH`, then run `podman compose up`. The `deploy` target detects whether the target is new or already installed and selects the matching mode automatically.

The server layout is flat: persistent state is in `SVC_PATH/data/`, and the transient reload socket is in `SVC_PATH/runtime/`. Updates preserve both. If an older installation has `SVC_PATH/deploy/`, the Makefile stops its containers when present, moves `data/` and custom `websites/` to the flat layout, and removes the legacy directory. It stops and reports a conflict instead of merging if both old and flat data or websites directories already exist.

The Makefile saves the image as a Docker-compatible archive, packages only the Compose file and mounted configs, copies the bundles over SSH, verifies checksums, loads and inspects the local image, then runs Compose with `--no-build`. Both services set Compose `pull_policy: never`, so the provider passes `--pull never` to Podman for each container. Do not pass `--pull never` to `podman compose up`: `podman-compose` 1.3.0 treats its command-level `--pull` as a flag and parses `never` as a service name. The target host needs Podman with a Compose provider plus SSH access; the SSH account must be able to write to `SVC_PATH` and run Podman.

You can create transfer archives without deploying by running `make -C deploy package`. They are written to `build/deploy-transfer/`.

Set `admin.nginx.admin_domain` in the repository's `deploy/admin.yaml` to the hostname that the host Nginx will forward before deployment. The deployed file is `SVC_PATH/admin.yaml`; each `install` or `update` replaces it from the repository copy, so keep the hostname change in `deploy/admin.yaml`. The UI only serves requests whose `Host` matches that value.

The admin entrypoint creates `SVC_PATH/data/master.key` on the first start. It keeps the key on later starts and refuses to generate a replacement if the key is missing while a database, release, or certificate already exists. The key is owned by root, readable by the `jasta-admin` group, and has mode `0640` as required by Jasta. Back up the entire `SVC_PATH/data` directory and preserve the key and its permissions.

After the first deployment has started, create the admin owner from an interactive terminal:

```sh
make -C deploy owner-bootstrap
```

This opens an SSH TTY and runs `jasta admin owner-bootstrap` in the running `admin` container as `jasta-admin`, using the mounted database and master key. Scan the displayed TOTP QR code, enter the current code, and save the recovery codes; they are shown only once.

Compose restarts admin after an explicit Compose-managed restart or update of web, so admin restores the active snapshot. If the container runtime restarts web automatically after a crash, it does not restart dependent services; run `podman compose -f /opt/jasta/compose.yaml restart admin` after web is healthy.

The bind-mounted `SVC_PATH/data` directory contains the SQLite database, master key, releases, certificates, and ACME challenge files. `SVC_PATH/runtime` contains the web reload socket. In the repository these are `deploy/data` and `deploy/runtime`; they are ignored by Git and excluded from the container build context and deployment archive.

On the server, use `podman compose -f /opt/jasta/compose.yaml logs -f web admin` to inspect service logs. For a local test, the default admin hostname is `admin.localhost`.

## Host Nginx

The host Nginx must proxy public site requests to `http://127.0.0.1:15432` and admin requests to `http://127.0.0.1:15433`, preserving the incoming `Host` header. Enable WebSocket upgrade forwarding for the admin endpoint. Ports are bound to loopback and are not exposed directly on external interfaces.

Nginx and TLS remain host-managed. The container config sets the Nginx validation and reload commands to `/bin/false`, so `nginx.apply` and ACME flows that reload Nginx are intentionally unavailable in this deployment. Configure and reload the host Nginx separately.

## Stop

```sh
podman compose -f /opt/jasta/compose.yaml down
```

This stops and removes the containers while preserving `/opt/jasta/data` and `/opt/jasta/runtime`.
