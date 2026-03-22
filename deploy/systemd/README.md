# Systemd service templates

This directory contains separate `systemd` units for `autocache` and for the `netcut` frontend/backend processes, using `/home/autocache` and `/home/netcut` as deployment roots.

## Files

- `deploy/systemd/autocache.service`: AutoCache RESP server unit.
- `deploy/systemd/netcut-backend.service`: Netcut backend service unit.
- `deploy/systemd/netcut-frontend.service`: Netcut frontend service unit.
- `deploy/systemd/autocache.env.example`: Example environment overrides for AutoCache.
- `deploy/systemd/netcut-backend.env.example`: Example backend command override for Netcut.
- `deploy/systemd/netcut-frontend.env.example`: Example frontend command override for Netcut.

## Install on a server

```bash
sudo install -d -o autocache -g autocache /home/autocache/bin /home/autocache/data
sudo install -d -o netcut -g netcut /home/netcut/backend /home/netcut/frontend /home/netcut/shared

sudo install -m 0644 deploy/systemd/autocache.service /etc/systemd/system/autocache.service
sudo install -m 0644 deploy/systemd/netcut-backend.service /etc/systemd/system/netcut-backend.service
sudo install -m 0644 deploy/systemd/netcut-frontend.service /etc/systemd/system/netcut-frontend.service

install -m 0644 deploy/systemd/autocache.env.example /home/autocache/autocache.env
install -m 0644 deploy/systemd/netcut-backend.env.example /home/netcut/backend/netcut-backend.env
install -m 0644 deploy/systemd/netcut-frontend.env.example /home/netcut/frontend/netcut-frontend.env

sudo systemctl daemon-reload
sudo systemctl enable --now autocache.service
sudo systemctl enable --now netcut-backend.service
sudo systemctl enable --now netcut-frontend.service
```

## What to customize

- `autocache.service`
  - Put the compiled binary at `/home/autocache/bin/autocache`.
  - Adjust `/home/autocache/autocache.env` for listen address, metrics address, data directory, config path, and extra flags.
  - The unit matches the flags exposed by `cmd/server/main.go`.
- `netcut-backend.service`
  - Replace `NETCUT_BACKEND_COMMAND` in `/home/netcut/backend/netcut-backend.env` with the exact backend launch command.
  - Example Node backend: `NETCUT_BACKEND_COMMAND="/usr/bin/npm run start"`
  - Example Go backend: `NETCUT_BACKEND_COMMAND="/home/netcut/backend/bin/netcut-backend --host 127.0.0.1 --port 8081"`
- `netcut-frontend.service`
  - Because your frontend is React, this template assumes you build static files into `/home/netcut/frontend/dist` and serve them as a process.
  - Replace `NETCUT_FRONTEND_COMMAND` in `/home/netcut/frontend/netcut-frontend.env` with the exact frontend launch command.
  - Recommended example: `NETCUT_FRONTEND_COMMAND="/usr/bin/npx serve -s /home/netcut/frontend/dist -l 3000"`

## Check service health

```bash
sudo systemctl status autocache.service
sudo systemctl status netcut-backend.service
sudo systemctl status netcut-frontend.service
sudo journalctl -u autocache.service -f
sudo journalctl -u netcut-backend.service -f
sudo journalctl -u netcut-frontend.service -f
```

## Notes

- If you later switch to Nginx for static hosting, you can drop `netcut-frontend.service` and let Nginx serve `/home/netcut/frontend/dist` directly.
- If you later switch to Nginx for static hosting, you can drop `netcut-frontend.service` and let Nginx serve `/home/netcut/frontend/dist` directly.
