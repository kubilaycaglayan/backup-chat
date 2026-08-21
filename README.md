# Backup Chat

A small two-person chat application using Go, WebSockets, and a local JSONL file. The frontend is plain HTML, CSS, and JavaScript embedded into the executable.

## Build and run

```bash
go build -o backup-chat
./backup-chat
```

Configuration defaults to `PORT=50000`, `DATA_FILE=./data/messages.jsonl`, and `RETENTION_DAYS=30`. The server listens on all interfaces. Open `http://SERVER_PUBLIC_IP:50000` in a browser after allowing the port through any applicable firewall, router, or cloud firewall.

To serve HTTPS directly from the Go application on the same port, set `TLS_CERT_FILE` and `TLS_KEY_FILE` to a trusted certificate and private key. When both are set, the application serves `https://SERVER_PUBLIC_IP:50000`; when omitted, it serves HTTP as before.

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

With certificate files mounted from the host:

```bash
docker run -d \
  --name backup-chat \
  --restart unless-stopped \
  -p 50000:50000 \
  -v backup-chat-data:/data \
  -v /etc/letsencrypt/live/SERVER_PUBLIC_IP:/certs:ro \
  -e TLS_CERT_FILE=/certs/fullchain.pem \
  -e TLS_KEY_FILE=/certs/privkey.pem \
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

## Push notifications

When supported, the app shows an **Enable notifications** prompt on first launch. Permission must be granted by pressing that button; browsers do not allow notification permission to be requested automatically. New messages notify subscribed devices belonging to the other nickname.

The server generates a VAPID key pair on first startup and stores it next to the configured message file in `messages.jsonl.vapid.json`. Keep that file with the message data so existing subscriptions remain valid. It is private and should not be committed or exposed. Alternatively, provide `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, and optionally `VAPID_SUBJECT` as environment variables.

For iPhone and iPad web push, set `VAPID_SUBJECT` to a real contact URI, such as `mailto:you@example.com` or `https://example.com`. Apple rejects localhost-style VAPID subjects with HTTP 403.

Push notifications require HTTPS on remote devices. iOS requires iOS 16.4 or later and the site must be installed on the Home Screen before push permission is available.
