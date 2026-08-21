# Backup Chat

A small two-person chat application using Go, WebSockets, and a local JSONL file. The frontend is plain HTML, CSS, and JavaScript embedded into the executable.

## Build and run

```bash
go build -o backup-chat
./backup-chat
```

Configuration defaults to `PORT=50000`, `DATA_FILE=./data/messages.jsonl`, and `RETENTION_DAYS=30`. The server listens on all interfaces. Open `http://SERVER_PUBLIC_IP:50000` in a browser after allowing the port through any applicable firewall, router, or cloud firewall.

## systemd

The included unit assumes the application is installed under `/opt/backup-chat`:

```bash
sudo mkdir -p /opt/backup-chat/data
sudo cp backup-chat /opt/backup-chat/backup-chat
sudo cp backup-chat.service /etc/systemd/system/backup-chat.service
sudo systemctl daemon-reload
sudo systemctl enable --now backup-chat
```

If needed, allow the default port with UFW:

```bash
sudo ufw allow 50000/tcp
```

Inspect application logs with:

```bash
sudo journalctl -u backup-chat -f
```

Persistent messages are stored in `data/messages.jsonl` for a local run, or `/opt/backup-chat/data/messages.jsonl` with the included systemd unit. Messages older than 30 days are removed at startup and approximately once per day using an atomic file replacement.

## Docker

Build and run the container with a named volume so messages survive container replacement:

```bash
docker build -t backup-chat .
docker volume create backup-chat-data
docker run -d \
  --name backup-chat \
  --restart unless-stopped \
  -p 50000:50000 \
  -v backup-chat-data:/data \
  backup-chat
```

Open `http://SERVER_PUBLIC_IP:50000`. Allow TCP port 50000 through any applicable firewall, router, or cloud firewall. View container logs with:

```bash
docker logs -f backup-chat
```

The Docker image stores persistent messages at `/data/messages.jsonl`, backed by the `backup-chat-data` volume. You can override configuration with `-e`, for example `-e RETENTION_DAYS=14`.

## Install as a PWA

The app includes a web manifest, service worker, standalone display mode, and home-screen icon. Remote phones must access it over HTTPS for service workers and offline app-shell caching; `localhost` is also treated as secure for local testing.

On iPhone or iPad, open the HTTPS URL in Safari or Chrome, use the Share menu, choose **Add to Home Screen**, and confirm. Open the new Home Screen icon to use the standalone app layout. The chat still requires a live connection to send and receive messages; the service worker only caches the application shell.
