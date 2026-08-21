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
