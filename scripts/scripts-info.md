
# The order of execution

## Install
preinst: install
postinst: configure

## Update
prerm: upgrade
preinst: upgrade
postrm: upgrade
postinst: configure

## Remove
prerm: remove   
postrm: remove 

## Purge
prerm: remove   
postrm: remove
postrm: purge 

## Jasta package lifecycle

- `preinst` stops and disables the previous single `jasta.service` during upgrade.
- `prerm` stops and disables both split services before upgrade or removal.
- `postinst configure` provisions system users and directories, reloads systemd, starts web then admin, and validates/reloads Nginx when it is running.
- `postrm remove` reloads systemd and Nginx after the package-owned Nginx shim has been removed. `remove` preserves `/var/lib/jasta` and `/etc/jasta/master.key`; only the explicit `purge` action deletes state and the master key.
- SQLite, releases, certificates, and ACME state are outside package-owned paths and are never cleaned by upgrade or ordinary removal.
