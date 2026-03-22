# Systemd service templates

This directory contains separate `systemd` units for `autocache` and `netcut`.

## Files

- `deploy/systemd/autocache.service`: AutoCache RESP server unit.
- `deploy/systemd/netcut.service`: Netcut service unit.
- `deploy/systemd/autocache.env.example`: Example environment overrides for AutoCache.
- `deploy/systemd/netcut.env.example`: Example command override for Netcut.

## Install on a server

```bash
sudo install -d /opt/autocache/bin /etc/autocache /var/lib/autocache
sudo install -d /opt/netcut /var/lib/netcut /var/log/netcut

sudo install -m 0644 deploy/systemd/autocache.service /etc/systemd/system/autocache.service
sudo install -m 0644 deploy/systemd/netcut.service /etc/systemd/system/netcut.service

sudo install -m 0644 deploy/systemd/autocache.env.example /etc/default/autocache
sudo install -m 0644 deploy/systemd/netcut.env.example /etc/default/netcut

sudo systemctl daemon-reload
sudo systemctl enable --now autocache.service
sudo systemctl enable --now netcut.service
```

## What to customize

- `autocache.service`
  - Put the compiled binary at `/opt/autocache/bin/autocache`.
  - Adjust `/etc/default/autocache` for listen address, metrics address, data directory, config path, and extra flags.
  - The unit matches the flags exposed by `cmd/server/main.go`.
- `netcut.service`
  - Replace `NETCUT_COMMAND` in `/etc/default/netcut` with the exact launch command for your app.
  - Example Node app: `NETCUT_COMMAND="/usr/bin/npm run start -- --host 127.0.0.1 --port 3000"`
  - Example Go app: `NETCUT_COMMAND="/opt/netcut/bin/netcut --host 127.0.0.1 --port 3000"`

## Check service health

```bash
sudo systemctl status autocache.service
sudo systemctl status netcut.service
sudo journalctl -u autocache.service -f
sudo journalctl -u netcut.service -f
```
