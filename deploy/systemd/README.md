# Systemd service templates

This directory contains separate `systemd` units for `autocache` and `netcut`, using `/home/autocache` and `/home/netcut` as deployment roots.

## Files

- `deploy/systemd/autocache.service`: AutoCache RESP server unit.
- `deploy/systemd/netcut.service`: Netcut service unit.
- `deploy/systemd/autocache.env.example`: Example environment overrides for AutoCache.
- `deploy/systemd/netcut.env.example`: Example command override for Netcut.

## Install on a server

```bash
sudo install -d -o autocache -g autocache /home/autocache/bin /home/autocache/data
sudo install -d -o netcut -g netcut /home/netcut/bin /home/netcut/shared

sudo install -m 0644 deploy/systemd/autocache.service /etc/systemd/system/autocache.service
sudo install -m 0644 deploy/systemd/netcut.service /etc/systemd/system/netcut.service

install -m 0644 deploy/systemd/autocache.env.example /home/autocache/autocache.env
install -m 0644 deploy/systemd/netcut.env.example /home/netcut/netcut.env

sudo systemctl daemon-reload
sudo systemctl enable --now autocache.service
sudo systemctl enable --now netcut.service
```

## What to customize

- `autocache.service`
  - Put the compiled binary at `/home/autocache/bin/autocache`.
  - Adjust `/home/autocache/autocache.env` for listen address, metrics address, data directory, optional config path, and extra flags.
  - Leave `AUTOCACHE_CONFIG=` empty if you do not have a standalone config file.
  - The unit matches the flags exposed by `cmd/server/main.go`.
- `netcut.service`
  - Replace `NETCUT_COMMAND` in `/home/netcut/netcut.env` with the exact launch command.
  - Example Node backend: `NETCUT_COMMAND="/usr/bin/npm run start"`
  - Example Go backend: `NETCUT_COMMAND="/home/netcut/bin/netcut-backend --host 127.0.0.1 --port 8081"`

## Check service health

```bash
sudo systemctl status autocache.service
sudo systemctl status netcut.service
sudo journalctl -u autocache.service -f
sudo journalctl -u netcut.service -f
```

## Notes

- This template assumes only the Netcut service process needs to be managed by systemd.
