## 2026-08-21 23:50 UTC — Move Backup Chat to the private Cloudflare Tunnel network

Purpose: replace the existing public-port Docker containers with the repository's
Docker Compose deployment, while preserving the `backup-chat-data` volume.

Planned commands:

```bash
docker stop backup-chat quirky_bhabha
docker rm backup-chat quirky_bhabha
. /home/ubuntu/.profile && docker compose up -d --build
```

Expected effects: removes only the verified `backup-chat` and `quirky_bhabha`
containers; retains the `backup-chat-data` volume; creates the private
`backup-chat-net` Docker network and Compose-managed chat and tunnel containers.
The chat port will no longer be published on the host.

Rollback: run the previous `backup-chat` image with
`-v backup-chat-data:/data -p 50000:50000`; run the Cloudflare connector again
with its tunnel token. The persistent volume is not removed by this change.

Result: succeeded. The old `backup-chat` and `quirky_bhabha` containers were
removed, the `backup-chat-data` volume was retained, and Compose created
`backup-chat-net` with replacement chat and Cloudflare Tunnel containers. The
chat container has no host-published port. HTTPS through the configured tunnel
returned a successful response.

## 2026-08-21 23:53 UTC — Redeploy the dark default interface

Purpose: rebuild and restart the chat container so the dark interface is served
through the existing Cloudflare Tunnel.

Planned command:

```bash
. /home/ubuntu/.profile && docker compose up -d --build
```

Expected effects: rebuilds the chat image and recreates the chat container if
needed. The `backup-chat-data` volume and tunnel configuration remain intact.

Rollback: deploy the previous Git revision with the same command. The
persistent volume is unaffected.

Result: succeeded. The chat image was rebuilt and its container was recreated;
the Cloudflare Tunnel container and persistent volume remained running. HTTPS
served the dark stylesheet and the updated service-worker cache version.

## 2026-08-22 00:01 UTC — Start the local development environment

Purpose: run the isolated development chat environment for local review before
deployment.

Planned command:

```bash
docker compose -f compose.dev.yaml up -d
```

Expected effects: creates or starts only the `backup-chat-dev` Compose project,
with a local-only `127.0.0.1:50001` port and the `backup-chat-dev-data` volume.
Production containers and the `backup-chat-data` volume are unaffected.

Rollback: run `docker compose -f compose.dev.yaml down`. To also remove
development messages and generated development push keys, use
`docker compose -f compose.dev.yaml down -v`.

Result: succeeded. The `backup-chat-dev` container is listening only on
`127.0.0.1:50001` and returned a successful HTTP response. The pre-existing
development volume was reused; production containers and data were unaffected.

## 2026-08-22 00:06 UTC — Restart the development environment

Purpose: compile and serve the updated local cache-control behavior.

Planned command:

```bash
docker compose -f compose.dev.yaml restart
```

Expected effects: restarts only the local `backup-chat-dev` chat container.
Production containers and persistent data are unaffected.

Rollback: run `docker compose -f compose.dev.yaml down`.

Result: succeeded. The development app is serving service-worker cache revision
`backup-chat-shell-v11` and the explicit five-row chat layout.

## 2026-08-22 00:35 UTC — Deploy notification layout update

Purpose: deploy the corrected in-chat notification prompt layout and PWA cache
revision to production.

Planned command:

```bash
docker compose up -d --build --no-deps backup-chat
```

Expected effects: rebuilds and replaces only the production chat container. The
Cloudflare Tunnel container, `backup-chat-net` network, and
`backup-chat-data` volume remain in place.

Rollback: rebuild and start the previous Git revision with the same command.
Persistent chat data is unaffected.

Result: succeeded. The production chat container was rebuilt and restarted.
Static assets now return `Cache-Control: no-store`, and the production service
worker has no fetch handler or application-shell precache. The Cloudflare
Tunnel container remained running and persistent chat data was retained.

## 2026-08-22 00:43 UTC — Deploy deterministic dark frontend assets

Purpose: restart development and deploy explicit dark canvas styling, versioned
frontend assets, and cache-bypassing service-worker updates.

Planned commands:

```bash
docker compose -f compose.dev.yaml restart
docker compose up -d --build --no-deps backup-chat
```

Expected effects: restarts the local development chat container, then rebuilds
and replaces only the production chat container. The Cloudflare Tunnel
container, networks, and persistent data volumes remain in place.

Rollback: rebuild and start the previous Git revision in production with the
same production command; stop development with
`docker compose -f compose.dev.yaml down`. Persistent chat data is unaffected.

Result: succeeded. Development and production now serve versioned `v12`
frontend assets, an explicit dark `html`/`body` canvas, and service-worker
registration with `updateViaCache: none`. Production static assets return
`Cache-Control: no-store`; the Cloudflare Tunnel and persistent data remained
in place.

Result: succeeded. The production chat container was rebuilt and restarted,
serving service-worker cache revision `backup-chat-shell-v11` and the explicit
five-row chat layout. The Cloudflare Tunnel container remained running and
persistent chat data was retained.

## 2026-08-22 00:38 UTC — Deploy PWA cache removal

Purpose: remove application-shell caching from the service worker so installed
PWAs use the current production assets, while retaining push notifications.

Planned command:

```bash
docker compose up -d --build --no-deps backup-chat
```

Expected effects: rebuilds and replaces only the production chat container. The
Cloudflare Tunnel container, `backup-chat-net` network, and
`backup-chat-data` volume remain in place.

Rollback: rebuild and start the previous Git revision with the same command.
Persistent chat data is unaffected.

Result: succeeded. The development app is serving service-worker cache revision
`backup-chat-shell-v10` and returned a successful local HTTP response.

## 2026-08-22 00:22 UTC — Redeploy the current production build

Purpose: rebuild and restart the production chat container so the current PWA
cache revision is served to installed clients.

Planned command:

```bash
docker compose up -d --build --no-deps backup-chat
```

Expected effects: rebuilds and replaces only the production chat container. The
Cloudflare Tunnel container, `backup-chat-net` network, and
`backup-chat-data` volume remain in place.

Rollback: rebuild and start the previous Git revision with the same command.
Persistent chat data is unaffected.

Result: succeeded. Docker confirmed that the production image already matched
the current working tree, so the chat container remained running. It serves
service-worker cache revision `backup-chat-shell-v10` and the current chat
notification prompt.

## 2026-08-22 00:34 UTC — Restart development for notification layout review

Purpose: compile and serve the corrected notification prompt layout and updated
PWA cache revision in the local development environment.

Planned command:

```bash
docker compose -f compose.dev.yaml restart
```

Expected effects: restarts only the local `backup-chat-dev` chat container.
Production containers and persistent data are unaffected.

Rollback: run `docker compose -f compose.dev.yaml down`.

Result: succeeded. The production chat container was rebuilt and restarted,
serving service-worker cache revision `backup-chat-shell-v10`. The Cloudflare
Tunnel container remained running and persistent chat data was retained.

## 2026-08-22 00:31 UTC — Deploy the latest application changes

Purpose: rebuild and restart the production chat container with the current
working tree.

Planned command:

```bash
docker compose up -d --build --no-deps backup-chat
```

Expected effects: rebuilds and replaces only the production chat container. The
Cloudflare Tunnel container, `backup-chat-net` network, and
`backup-chat-data` volume remain in place.

Rollback: rebuild and start the previous Git revision with the same command.
Persistent chat data is unaffected.

Result: succeeded. The local stylesheet response now includes
`Cache-Control: no-cache`.

## 2026-08-22 00:08 UTC — Deploy browser-cache revalidation

Purpose: deploy the verified asset revalidation fix so production users receive
updated frontend assets without a hard refresh.

Planned command:

```bash
docker compose up -d --build --no-deps backup-chat
```

Expected effects: rebuilds and replaces only the production chat container. The
Cloudflare Tunnel container, `backup-chat-net` network, and
`backup-chat-data` volume remain in place.

Rollback: rebuild and start the previous Git revision with the same command.
Persistent chat data is unaffected.

Result: succeeded. The production chat container was rebuilt and restarted; its
stylesheet response includes `Cache-Control: no-cache`. The Cloudflare Tunnel
container remained running and the persistent chat data volume was retained.

## 2026-08-22 00:15 UTC — Restart the development environment

Purpose: compile the updated PWA cache, nickname, and notification behavior for
local review.

Planned command:

```bash
docker compose -f compose.dev.yaml restart
```

Expected effects: restarts only the local `backup-chat-dev` chat container.
Production containers and persistent data are unaffected.

Rollback: run `docker compose -f compose.dev.yaml down`.
