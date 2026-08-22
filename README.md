# Backup Chat

A small two-person chat application using Go, WebSockets, and a local JSONL file. The frontend is plain HTML, CSS, and JavaScript embedded into the executable.

## Build and run

```bash
go build -o backup-chat
./backup-chat
```

Configuration defaults to `PORT=50000`, `DATA_FILE=./data/messages.jsonl`, and `RETENTION_DAYS=30`. For local development, open `http://localhost:50000` in a browser.

## Environment variables

Available configuration and deployment variable names:

```text
PORT
DATA_FILE
RETENTION_DAYS
TLS_CERT_FILE
TLS_KEY_FILE
VAPID_PUBLIC_KEY
VAPID_PRIVATE_KEY
VAPID_SUBJECT
CHAT_HOSTNAME
CLOUDFLARE_TUNNEL_TOKEN
```

## Development environment

Use the separate development Compose file before deploying a change. It runs
locally on port 50001, does not start Cloudflare Tunnel, and stores its messages
in a separate `backup-chat-dev-data` Docker volume.

```bash
docker compose -f compose.dev.yaml up
```

Open `http://localhost:50001`. The repository is mounted into the container, so
the next start uses your current source files. After changing Go code or an
embedded frontend file, stop the command with `Ctrl+C` and run it again to
rebuild and restart the application.

To discard only development chat data, run:

```bash
docker compose -f compose.dev.yaml down -v
```

The production Compose configuration below uses a different volume and is not
affected by this command.

For the deployed service, use the Cloudflare Tunnel configuration below.
Cloudflare provides public HTTPS and forwards the connection to the private HTTP
service in Docker.

## systemd

The included unit assumes the application is installed under `/opt/backup-chat`:

```bash
sudo mkdir -p /opt/backup-chat/data
sudo cp backup-chat /opt/backup-chat/backup-chat
sudo cp backup-chat.service /etc/systemd/system/backup-chat.service
sudo systemctl daemon-reload
sudo systemctl enable --now backup-chat
```

Inspect application logs with:

```bash
sudo journalctl -u backup-chat -f
```

Persistent messages are stored in `data/messages.jsonl` for a local run, or `/opt/backup-chat/data/messages.jsonl` with the included systemd unit. Messages older than 30 days are removed at startup and approximately once per day using an atomic file replacement.

## Docker deployment through Cloudflare Tunnel

To publish the chat through a Cloudflare Tunnel, do not expose port 50000 with
`-p`. The chat and tunnel share a private Docker network, while `cloudflared`
makes the outbound connection to Cloudflare. This means no router port
forwarding or public firewall rule is required for the chat port.

In Cloudflare, create a named tunnel and add a published application route with
the hostname stored in `CHAT_HOSTNAME` and service URL
`http://backup-chat:50000`. Wait until the Cloudflare zone is **Active** before
using the route.

The VAPID variables and tunnel token must be exported in the shell that runs
Docker Compose; do not put their values in this repository. `VAPID_SUBJECT`
must be a contact URI, such as `mailto:you@example.com` or
`https://$CHAT_HOSTNAME`, not the bare hostname.

```bash
export CHAT_HOSTNAME='chat.example.com'
export VAPID_SUBJECT='mailto:you@example.com'
read -r -s -p "Cloudflare tunnel token: " CLOUDFLARE_TUNNEL_TOKEN
echo
export CLOUDFLARE_TUNNEL_TOKEN
docker volume inspect backup-chat-data >/dev/null 2>&1 || docker volume create backup-chat-data
docker compose up -d --build
unset CLOUDFLARE_TUNNEL_TOKEN
```

The Compose file creates the private `backup-chat-net` network and persistent
`backup-chat-data` volume. The volume preserves messages, VAPID keys, and push
subscriptions across container replacement. Neither container publishes port
50000 to the host.

Check the deployment with:

```bash
docker compose ps
docker compose logs --tail=100 cloudflared backup-chat
```

## Install as a PWA

The app includes a web manifest, service worker, standalone display mode, and home-screen icon. Remote phones must access it over HTTPS for service workers and offline app-shell caching; `localhost` is also treated as secure for local testing.

On iPhone or iPad, open the HTTPS URL in Safari or Chrome, use the Share menu, choose **Add to Home Screen**, and confirm. Open the new Home Screen icon to use the standalone app layout. The chat still requires a live connection to send and receive messages; the service worker only caches the application shell.

## Push notifications

When supported, the app shows an **Enable notifications** prompt on first launch. Permission must be granted by pressing that button; browsers do not allow notification permission to be requested automatically. New messages notify subscribed devices belonging to the other nickname.

The server generates a VAPID key pair on first startup and stores it next to the configured message file in `messages.jsonl.vapid.json`. Keep that file with the message data so existing subscriptions remain valid. It is private and should not be committed or exposed. Alternatively, provide `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, and optionally `VAPID_SUBJECT` as environment variables.

For iPhone and iPad web push, set `VAPID_SUBJECT` to a real contact URI, such as `mailto:you@example.com` or `https://example.com`. Apple rejects localhost-style VAPID subjects with HTTP 403.

Push notifications require HTTPS on remote devices. iOS requires iOS 16.4 or later and the site must be installed on the Home Screen before push permission is available.
