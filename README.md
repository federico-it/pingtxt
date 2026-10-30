# pingtxt

Minimal, terminal-flavoured uptime monitoring. Think Uptime Kuma / UptimeRobot / HetrixTools,
but plain text, monospace, light and dark.

- **One container** for the hub: Go API + scheduler + SQLite + the Next.js UI, ~25 MB image.
- **Agents** for multi-location checks: a ~7 MB static binary (or Docker image) that only
  dials *out* to the hub over WebSocket. No open ports, works behind NAT. Install with one command.
- Checks: **HTTP(S)** (status codes, headers, body, TLS expiry), **keyword**, **TCP port**, **ICMP ping**, **DNS**.
- Status across locations: **down** when the majority of online locations fail, **degraded** when only some do.
- Incidents, uptime 24h/7d/30d, response time per location, raw check log.
- Notifications: webhook (JSON), Discord, Slack, Telegram, ntfy, email (SMTP with STARTTLS/TLS, configured from the UI).
- Public **status pages** with the monitors you pick, daily uptime history and incidents, on `/status/<slug>/` or a custom domain.

| dark | light |
| --- | --- |
| ![dark](docs/screenshot-dark.png) | ![light](docs/screenshot-light.png) |

## Run the hub

```sh
docker run -d --name pingtxt --restart unless-stopped \
  -p 8090:8090 -v ./data:/data \
  ghcr.io/federico-it/pingtxt:latest
```

or `docker compose up -d` with the [docker-compose.yml](docker-compose.yml) in this repo.
Open `http://localhost:8090`, create the admin account, add monitors.

Everything else is configured from the web UI under **settings** and stored in the database:
public URL, built-in location, retention, defaults for new monitors, users and login.
Only a few env vars are read at startup:

| env | default | |
| --- | --- | --- |
| `PINGTXT_ADDR` | `:8090` | listen address |
| `PINGTXT_DATA` | `/data` | SQLite database directory |

`PINGTXT_PUBLIC_URL`, `PINGTXT_RETENTION_DAYS`, `PINGTXT_LOCAL_NAME` and `PINGTXT_DISABLE_LOCAL`
still work, but only seed the settings on the first start. After that, change them in the UI.

### Single sign-on (GitHub, Google, OpenID Connect)

In **settings → authentication** turn on a provider, paste the client id and secret, and register
the callback URL shown there (`<public url>/api/auth/oauth/<provider>/callback`) at the provider.
The generic OpenID Connect option works with Authentik, Keycloak, Authelia, Pocket ID, Zitadel, Okta, Entra ID and others.

- A provider login is matched to an existing user by verified email, then linked. Users can also
  connect accounts from **settings → account**.
- Turn on *create accounts on first single sign-on* to let new people in, optionally limited to some email domains.
- You can turn off password login once a provider works. If you lock yourself out, run
  `pingtxt -enable-password-login` (in Docker: `docker exec pingtxt pingtxt -enable-password-login`, then restart).
- Set the **public URL** in settings when the hub is behind a reverse proxy, so callback URLs are right.

Put it behind a reverse proxy with TLS (Caddy, Traefik, nginx). WebSocket upgrade must be allowed on `/api/agent/ws`.

## Status pages

Create them under **status** in the web UI: pick the monitors, their order, and whether past incidents are shown.
A page is public at `https://<hub>/status/<slug>/`. Visitors only see monitor names, status, uptime and incident
times, never targets, locations or error messages. Drafts are visible only to logged-in users.

For a **custom domain** (e.g. `status.example.com`), set it on the page, point its DNS at the hub and route the host
to pingtxt in your reverse proxy (with its own TLS certificate). On that host pingtxt serves only the status page at
`/`; the admin UI and API are not reachable there. The history covers the retention period, up to 90 days.

## Add a location (agent)

In the UI go to **agents → add agent**: you get a token and ready-to-paste commands.

```sh
# Docker
docker run -d --name pingtxt-agent --restart unless-stopped --cap-add NET_RAW \
  -e PINGTXT_HUB=https://status.example.com -e PINGTXT_TOKEN=pt_xxx \
  ghcr.io/federico-it/pingtxt-agent:latest

# Linux + systemd (binary downloaded from your hub)
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --hub https://status.example.com --token pt_xxx
# uninstall
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --uninstall
```

Then tick the new location in any monitor. The hub schedules every check and sends it to the
agents; agents are stateless and reconnect with backoff.

## Architecture

```
            ┌──────────────── hub container ────────────────┐
 browser ──▶│  Go http server ─ /api/*  ─ SQLite (WAL)      │
            │        │          /        ─ Next.js export   │
            │   scheduler ──▶ local probe ("hub" location)  │
            │        │                                       │
            └────────┼───────────────────────────────────────┘
                     │ jobs / results over WebSocket (agent dials out)
          ┌──────────┴──────────┐
     agent (Frankfurt)     agent (home lab) ...
```

- `cmd/pingtxt` – hub binary. `cmd/pingtxt-agent` – agent binary.
- `internal/check` – probe implementations, shared by hub and agents.
- `internal/hub` – scheduler, status evaluation, incidents, notifications, HTTP API, agent WebSocket.
- `internal/store` – SQLite (pure Go driver `modernc.org/sqlite`, no CGO).
- `web/` – Next.js 16 (App Router, static export), shadcn/ui on Base UI, Tailwind 4.

**Why Go for the backend:** the agent must be a tiny static binary that runs anywhere
(amd64/arm64/arm, Linux/macOS) — the same language lets the hub and the agents share the probe code
and the wire protocol. Go's concurrency fits thousands of periodic checks, and the hub
compiles to one binary with the UI embedded, so the container needs no Node runtime.

## Development

```sh
make dev-hub          # Go hub on :8090 (data in ./data)
make dev-web          # next dev on :3000, proxies /api to :8090
make test lint
make web hub agents   # production build into ./dist
```
