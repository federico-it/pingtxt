# pingtxt

Minimal, terminal-flavoured uptime monitoring. Website and docs: [pingtxt.com](https://pingtxt.com). Think Uptime Kuma / UptimeRobot / HetrixTools,
but plain text, monospace, light and dark.

- **One container** for the hub: Go API + scheduler + SQLite + the Next.js UI, ~25 MB image.
- **Agents** for multi-location checks: a ~8 MB static binary (or Docker image) that only
  dials *out* to the hub over WebSocket. No open ports, works behind NAT. Install with one command.
- Checks: **HTTP(S)** (status codes, headers, body, JSON response rules, TLS expiry), **keyword**, **TCP port**, **ICMP ping**, **DNS**,
  databases (**PostgreSQL**, **MySQL/MariaDB**, **Redis**, **MongoDB**), **Docker containers**, **gRPC health**, **SMTP**,
  remote **MCP servers** (handshake, tools), and **heartbeat (push)** monitors for cron jobs and workers.
- Status across locations: **down** when more than half of the online locations fail, **degraded** when at least 35% fail (configurable in settings and per monitor).
- Incidents, uptime 24h/7d/30d, response time per location, raw check log.
- Notifications: webhook (JSON), Discord, Slack, Telegram, ntfy, email (SMTP with STARTTLS/TLS, configured from the UI).
- Public **status pages** with the monitors you pick, daily uptime history and incidents, on `/status/<slug>/` or a custom domain.
- Login with password (+ TOTP two-factor), GitHub, Google or any OpenID Connect provider. Personal API tokens.
- **Groups and tags**: a monitor sits in one group and has any number of colored tags. Filter the dashboard by
  them, show groups as sections on the dashboard and on status pages, and pick monitors by group or tag in
  maintenance windows.
- **Maintenance windows**, one-off or recurring (daily, weekly, monthly, in any time zone), for some or all monitors
  (or whole groups and tags):
  checks keep running but no incidents or alerts are raised, and those checks don't count toward uptime.
- **Expiry alerts** for TLS certificates (https monitors) and domain registrations (RDAP, with WHOIS
  fallback for registries without RDAP such as .it). Thresholds in days are set in settings
  (default 14/7/3/1 for certificates, 30/14/7/1 for domains): one alert per threshold crossed,
  one when it expires, one when it is renewed.
- **Prometheus** metrics on `/metrics` (status, latency per location, uptime, certificate and domain expiry, agents).
- **Backups**: full database snapshots on demand or on a schedule (with retention), downloadable and restorable
  from the UI, plus a **configuration export/import** in JSON to copy monitors and settings between hubs.

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
| `PINGTXT_DB_URL` | | PostgreSQL or MariaDB/MySQL instead of SQLite, see below |

`PINGTXT_PUBLIC_URL`, `PINGTXT_RETENTION_DAYS`, `PINGTXT_LOCAL_NAME` and `PINGTXT_DISABLE_LOCAL`
still work, but only seed the settings on the first start. After that, change them in the UI.

### Single sign-on (GitHub, Google, OpenID Connect)

In **settings → authentication** turn on a provider, paste the client id and secret, and register
the callback URL shown there (`<public url>/api/auth/oauth/<provider>/callback`) at the provider.
The generic OpenID Connect option works with Authentik, Keycloak, Authelia, Pocket ID, Zitadel, Okta, Entra ID and others.

- A provider login is matched to an existing user by verified email, then linked. Users can also
  connect accounts from **settings → account**.
- Turn on *create accounts on first single sign-on* to let new people in, optionally limited to some email domains.
  New accounts get the role picked there (read-only by default).
- You can turn off password login once a provider works. If you lock yourself out, run
  `pingtxt -enable-password-login` (in Docker: `docker exec pingtxt pingtxt -enable-password-login`, then restart).
- Set the **public URL** in settings when the hub is behind a reverse proxy, so callback URLs are right.

### Two-factor authentication

In **settings → two-factor authentication** scan the QR code with an authenticator app
(1Password, Bitwarden, Aegis, Google Authenticator...) and confirm a code. You get 10 single-use
recovery codes. After that, password logins ask for a code; single sign-on logins rely on the provider's own MFA.

- Lost phone and recovery codes: an admin can click **reset 2fa** in **settings → users**, or run
  `pingtxt -reset-2fa you@example.com` on the server (in Docker: `docker exec pingtxt pingtxt -reset-2fa you@example.com`).

### Users and roles

Every user is an **admin** or **read-only**. Admins add users in **settings → users** (with a password, or without one
to log in through single sign-on with the same email) and change roles there.

- **Read-only** users see monitors, incidents, history, agents, maintenance and status pages, but every change is
  refused by the API and the buttons are hidden. Passwords in monitor targets, request headers and bodies, push URLs
  and notification channel settings are hidden from them. They can still change their own password, 2FA and read tokens.
- Instance settings and users are for admins only. Admins cannot change their own role, so one admin always remains.
- Users that existed before roles were added are admins. Locked out? `pingtxt -make-admin you@example.com`.

### API tokens

Create personal tokens in **settings → api tokens** and send them as a bearer token:

```sh
curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/monitors
```

- **read only** tokens can call `GET` endpoints (monitors, incidents, agents, notifiers).
  **read + write** tokens can also create, edit, pause and delete.
- A token never has more rights than its owner: read-only users only get read tokens, and the write tokens of an
  admin who becomes read-only stop writing.
- Tokens can expire after 30, 90 or 365 days, or never. They are shown once and stored hashed.
- Tokens never reach account endpoints (password, 2FA, tokens, users, instance settings): those need a browser login.

`GET /api/monitors` accepts `?group=<id>` (or `group=none`) and `?tag=<id>` filters. Groups and tags are managed at
`/api/groups` and `/api/tags`; `PUT /api/groups/<id>/monitors` with `{"monitor_ids": [...]}` sets a group's members.

### Prometheus

`GET /metrics` serves metrics in the Prometheus text format. It needs an API token (read scope is enough),
unless **settings → prometheus metrics → public /metrics** is on.

```yaml
scrape_configs:
  - job_name: pingtxt
    scheme: https
    authorization:
      credentials: ptk_...
    static_configs:
      - targets: ["status.example.com"]
```

| metric | labels | meaning |
| --- | --- | --- |
| `pingtxt_monitor_up` | monitor_id, monitor, type | 1 up or degraded, 0 down (absent while pending, paused, in maintenance) |
| `pingtxt_monitor_status` | + status | 1 for the current status (up, down, degraded, pending, paused, maintenance) |
| `pingtxt_monitor_incident_open` | monitor | 1 while an incident is open |
| `pingtxt_monitor_uptime_ratio` | + window (24h, 7d, 30d) | share of successful checks, maintenance excluded |
| `pingtxt_monitor_latency_avg_seconds` | + window | average response time of successful checks |
| `pingtxt_check_up`, `pingtxt_check_latency_seconds`, `pingtxt_check_status_code`, `pingtxt_check_timestamp_seconds` | + location, location_name | last check from each location |
| `pingtxt_cert_expiry_days`, `pingtxt_domain_expiry_days` (and `_timestamp_seconds`) | + subject | days left before expiry |
| `pingtxt_agent_connected`, `pingtxt_agent_last_seen_timestamp_seconds` | agent_id, agent, location, location_name, version | agent connectivity |
| `pingtxt_monitors`, `pingtxt_agents`, `pingtxt_incidents_open`, `pingtxt_info` | | totals and hub version |

The output is cached for 10 seconds.

Put it behind a reverse proxy with TLS (Caddy, Traefik, nginx). WebSocket upgrade must be allowed on `/api/agent/ws`.

## Status pages

Create them under **status** in the web UI: pick the monitors, their order, and whether past incidents are shown.
You can also add whole **groups**: every monitor of the group is shown, including ones added later, and each group
is a section with its own status on the page.
A page is public at `https://<hub>/status/<slug>/`. Visitors only see monitor names, status, uptime and incident
times, never targets, locations or error messages. Drafts are visible only to logged-in users.

For a **custom domain** (e.g. `status.example.com`), set it on the page, point its DNS at the hub and route the host
to pingtxt in your reverse proxy (with its own TLS certificate). On that host pingtxt serves only the status page at
`/`; the admin UI and API are not reachable there. The history covers the retention period, up to 90 days.
## Heartbeat monitors (cron jobs)

For jobs that run on a schedule (backups, cron, queues) create a monitor of type **heartbeat (push)**. It gets a secret URL; the job calls it at the end of every run, with any HTTP method:

```sh
0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 https://pingtxt.example.com/api/push/<token> >/dev/null
```

The monitor goes down (incident + notifications) when no call arrives within the **expected period** plus the **grace time**, and back up at the next call. Optional query parameters: `status=down` reports a failed run, `msg=` stores a short note, `ping=` the run time in ms. The URL uses the public URL from settings and can be regenerated from the monitor page.

## Databases, containers, gRPC and SMTP

| type | target | what is checked |
| --- | --- | --- |
| postgresql | `postgres://user:pass@host:5432/db?sslmode=disable` | connect and run a query (default `SELECT 1`); optional expected value of the first column |
| mysql / mariadb | `mysql://user:pass@host:3306/db` (`?tls=true\|false\|skip-verify`) | same as postgresql |
| redis | `redis://[user]:pass@host:6379/0`, `rediss://` for TLS | `AUTH` + `PING` |
| mongodb | `mongodb://user:pass@host:27017/?authSource=admin`, `mongodb+srv://` | `hello`, SCRAM-SHA-256/SHA-1 auth, `ping`; shows primary/secondary |
| docker container | container name or id | down when not running, restarting, paused or `unhealthy` |
| grpc health | `grpc://host:port[/service]`, `grpcs://` for TLS | `grpc.health.v1.Health/Check` must return `SERVING` |
| smtp | `smtp://host:25`, `smtp://host:587`, `smtps://host:465` | greeting, `EHLO`, `STARTTLS` when offered (certificate verified) |

Passwords in connection URLs are hidden in the monitor list and in notifications; use a read-only
database user. Postgres, MySQL and MongoDB TLS default to "use it when the server offers it".

Docker checks talk to the Docker Engine of the location that runs them: the hub or the agent needs
`/var/run/docker.sock` (or `DOCKER_HOST`). With Docker, add
`-v /var/run/docker.sock:/var/run/docker.sock:ro` to the hub or agent container. For an agent
installed with systemd, allow it with `sudo systemctl edit pingtxt-agent`:

```ini
[Service]
SupplementaryGroups=docker
```

Access to the Docker socket is equivalent to root on that machine (`:ro` does not limit the API):
pingtxt only reads `GET /containers/<name>/json`, but enable it only where you need container checks.
Agents older than this version report these types as `unknown monitor type`: update them.

### PostgreSQL or MariaDB

SQLite is the default and needs nothing else. To keep the data in PostgreSQL or MariaDB/MySQL instead,
set `PINGTXT_DB_URL`. The database must exist and be empty (or hold a pingtxt schema); tables are created
and migrated at start.

```sh
# PostgreSQL 13+
PINGTXT_DB_URL=postgres://pingtxt:secret@db:5432/pingtxt?sslmode=disable
# MariaDB 10.6+ or MySQL 8.0.13+
PINGTXT_DB_URL=mariadb://pingtxt:secret@db:3306/pingtxt     # mysql://... works too
```

Ready-made compose files start the hub and the database with one command:

```sh
curl -O https://raw.githubusercontent.com/federico-it/pingtxt/main/docker-compose.postgres.yml
echo "DB_PASSWORD=$(openssl rand -hex 16)" > .env
docker compose -f docker-compose.postgres.yml up -d

curl -O https://raw.githubusercontent.com/federico-it/pingtxt/main/docker-compose.mariadb.yml
printf "DB_PASSWORD=%s\nDB_ROOT_PASSWORD=%s\n" $(openssl rand -hex 16) $(openssl rand -hex 16) > .env
docker compose -f docker-compose.mariadb.yml up -d
```

The compose files refuse to start without the passwords in `.env`; the database container uses them only
when it first creates the database, so set them before the first start. Special characters in a password must be URL-encoded in `PINGTXT_DB_URL`. Everything works the
same on the three databases (history rollups, Prometheus metrics, configuration export/import), except
the built-in **database backups**, which are SQLite files: with PostgreSQL or MariaDB back up the server
with its own tools.

```sh
# PostgreSQL
docker exec pingtxt-postgres pg_dump -U pingtxt -Fc pingtxt > pingtxt.dump
docker stop pingtxt
docker exec -i pingtxt-postgres pg_restore -U pingtxt -d pingtxt --clean --if-exists < pingtxt.dump
docker start pingtxt

# MariaDB
docker exec pingtxt-mariadb sh -c 'mariadb-dump -u pingtxt -p"$MARIADB_PASSWORD" --single-transaction pingtxt' > pingtxt.sql
docker stop pingtxt
docker exec -i pingtxt-mariadb sh -c 'mariadb -u pingtxt -p"$MARIADB_PASSWORD" pingtxt' < pingtxt.sql
docker start pingtxt
```

To move an existing SQLite hub to PostgreSQL or MariaDB, export the configuration *with secrets* from the
old hub and import it in the new one (history is not moved).

## MCP servers

A monitor of type **mcp server** checks a remote [Model Context Protocol](https://modelcontextprotocol.io) server:

| setting | |
| --- | --- |
| target | URL of the endpoint, e.g. `https://mcp.example.com/mcp` |
| transport | `auto` (default): streamable HTTP, falling back to the legacy HTTP+SSE transport (2024-11-05) when the POST is rejected with 400/404/405/406/415; or force `streamable` / `sse` |
| headers | sent with every request, e.g. `Authorization: Bearer <token>`; hidden from read-only users |
| required tools | optional, comma-separated: down when one of them is not in `tools/list` |
| minimum tools | optional: down when `tools/list` returns fewer tools |

Each check runs `initialize`, sends `notifications/initialized`, then `tools/list` (all pages) when the server
advertises tools or a tool condition is set, and finally deletes the session. Tools are never called. The check
message shows the server name and version, the negotiated protocol version, the transport and the tool count,
e.g. `my-server 1.4.0 · mcp 2025-06-18 · streamable · 12 tools`; the response time covers the whole handshake.
https endpoints get certificate expiry alerts. Agents older than this version report `unknown monitor type: mcp`:
update them.

## Backups and configuration export

With SQLite, settings → **database backups** takes a consistent copy of the whole database (settings, users,
secrets, history) while the hub keeps running. Turn on **scheduled backups** to take one every N hours and
keep the last N. Files live in `<data>/backups` (`/data/backups` in Docker) and can be downloaded or
uploaded from the same page. A backup is also taken automatically before every configuration import and
before every restore.

**Restore** replaces the whole database. In the UI pick a backup → *restore*: it is staged and swapped in
when the hub restarts (`docker restart pingtxt`). From a shell, with the hub stopped:

```sh
pingtxt -restore /data/backups/pingtxt-auto-20261030-020000.db
# Docker: docker compose stop pingtxt
#         docker compose run --rm pingtxt -restore /data/backups/<file>.db
#         docker compose start pingtxt
```

The replaced database is kept as `pingtxt-pre-restore-*.db`. Backups from older versions are migrated on
start; backups from a newer version are refused. To take a backup from cron instead:
`pingtxt -backup /path/to/file.db` (safe while the hub runs; in Docker `docker exec pingtxt pingtxt -backup /data/backups/x.db`).

Settings → **configuration export** downloads a JSON file with monitors, groups, tags, notification
channels, maintenance windows, status pages, agents and instance settings — no history and no user
accounts. Without *include secrets*, passwords, bot tokens, secret webhook URLs (Discord, Slack), login
client secrets, credential headers and passwords in target URLs are replaced by `[redacted]`, and agent
and heartbeat tokens are left out. Importing:

- **merge** updates objects with the same name (status pages by slug) and adds the rest; redacted values
  keep what is stored. **replace** deletes current monitors (with their history), groups, tags, channels,
  maintenance windows and status pages first. Agents are never deleted.
- On another hub, channels with redacted secrets are skipped and monitors with redacted credentials are
  paused; the import result lists what to fill in. Export *with* secrets to move a hub completely: agents
  keep their tokens and heartbeat URLs stay the same.

The same is available from the API and the CLI:

```sh
curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/config/export > pingtxt.json   # no secrets with a token
pingtxt -export-config pingtxt.json [-export-secrets]
pingtxt -import-config pingtxt.json [-import-mode replace]   # then restart the hub
```

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

### Agent updates

The hub bundles agent binaries built with its own version. When you upgrade the hub, agents
installed with the script update themselves as soon as they reconnect: the hub sends the
download path and the SHA-256 of the binary for the agent's platform, the agent downloads it
from the hub, checks the checksum and that the new binary reports the expected version, swaps
the file and restarts in place. A failed update leaves the old binary running and is shown on
the agents page.

- **agents → update agents automatically** turns this on or off (on by default). Each outdated
  agent also gets an **update** button.
- Agents installed before this feature run as a read-only systemd `DynamicUser`: re-run the
  install script once. It now installs to `/opt/pingtxt-agent`, owned by a `pingtxt-agent` user.
- To keep an agent on its current version, start it with `-no-auto-update`
  (or `PINGTXT_NO_AUTO_UPDATE=1` in `/etc/pingtxt-agent.env`).
- **Docker agents** never replace their own binary: pull the new image, or let
  [Watchtower](https://containrrr.dev/watchtower/) (or a similar image updater) do it:

  ```sh
  docker run -d --name watchtower --restart unless-stopped \
    -v /var/run/docker.sock:/var/run/docker.sock \
    containrrr/watchtower pingtxt-agent --interval 3600
  ```

Pushing a `v*` tag publishes a GitHub release with the hub and agent binaries and `SHA256SUMS`,
plus the Docker images for that tag.

## Build from source

You need **Go 1.26+** (the version in [go.mod](go.mod)), **Node.js 22+** with npm, `make` and `git`.
No C compiler: everything is built with `CGO_ENABLED=0`. Docker is only needed for the images.

```sh
git clone https://github.com/federico-it/pingtxt.git && cd pingtxt
git checkout v1.2.3        # optional: a release tag; main is the development version
make web hub agent agents
```

| target | output | |
| --- | --- | --- |
| `make web` | `internal/ui/dist` | builds the Next.js UI (`web/out`) and copies it where the hub embeds it. Run it before `make hub`, or the hub has no web UI |
| `make hub` | `dist/pingtxt` | the hub, with the UI inside, for this machine |
| `make agent` | `dist/pingtxt-agent` | the agent for this machine |
| `make agents` | `dist/agents/pingtxt-agent-<os>-<arch>` | agents for linux amd64/arm64/arm and darwin amd64/arm64, served by the hub for the install script and agent updates |

The version comes from `git describe` (`v1.2.3`, or the commit on main); set it with `make hub agents VERSION=v1.2.3`.
Agents update themselves to the hub version, so build hub and agents from the same checkout.

Run the hub from the repository folder, or point it at the data and agent folders:

```sh
./dist/pingtxt     # data in ./data, agents from ./dist/agents, UI on :8090
PINGTXT_DATA=/var/lib/pingtxt PINGTXT_AGENT_DIR=/usr/local/share/pingtxt/agents /usr/local/bin/pingtxt
```

Outside Docker, ICMP **ping** checks need raw sockets: `sudo setcap cap_net_raw+ep /usr/local/bin/pingtxt`
(same for `pingtxt-agent`), or set `PINGTXT_UNPRIVILEGED_PING=1` and allow the user's group in
`net.ipv4.ping_group_range`. A minimal systemd unit for the hub:

```ini
# /etc/systemd/system/pingtxt.service
[Unit]
Description=pingtxt hub
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/pingtxt
Environment=PINGTXT_DATA=/var/lib/pingtxt PINGTXT_AGENT_DIR=/usr/local/share/pingtxt/agents
StateDirectory=pingtxt
DynamicUser=yes
AmbientCapabilities=CAP_NET_RAW
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

**Cross-compiling** for another platform (the UI is the same for all of them, build it once with `make web`):

```sh
V=$(git describe --tags --always)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist/pingtxt-linux-arm64 ./cmd/pingtxt
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist/pingtxt-agent-linux-armv7 ./cmd/pingtxt-agent
```

**Docker images.** The Dockerfiles build everything inside the image (Node and Go included), so only Docker is needed:

```sh
make docker                                   # pingtxt:dev and pingtxt-agent:dev for this machine
docker build --build-arg VERSION=$(git describe --tags --always) -t pingtxt:dev .
docker build --build-arg VERSION=$(git describe --tags --always) -f Dockerfile.agent -t pingtxt-agent:dev .
# both architectures, pushed to your registry
docker buildx build --platform linux/amd64,linux/arm64 -t registry.example.com/pingtxt:dev --push .
```

To run the compose files with an image built from your checkout, add
[docker-compose.build.yml](docker-compose.build.yml) after any of them. It works the same with SQLite,
PostgreSQL and MariaDB (create the `.env` with the passwords first, as above):

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.postgres.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.mariadb.yml -f docker-compose.build.yml up -d --build
```

## Architecture

```
            ┌──────────────── hub container ────────────────┐
 browser ──▶│  Go http server ─ /api/*  ─ SQLite/PG/MariaDB │
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
- `internal/expiry` – certificate and domain (RDAP / WHOIS) expiry lookups, run by the hub.
- `internal/hub` – scheduler, status evaluation, incidents, notifications, HTTP API, agent WebSocket.
- `internal/store` – SQLite (pure Go driver `modernc.org/sqlite`, no CGO), PostgreSQL (`lib/pq`) and
  MariaDB/MySQL (`go-sql-driver/mysql`). Queries are written once for SQLite and rewritten for the other
  two; PostgreSQL and MariaDB start from one base schema plus the later migrations.
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

The store and hub tests run on SQLite by default. To run them on a database server (each test creates and
drops its own scratch database, so the user needs `CREATE DATABASE`):

```sh
docker run -d --name pg -p 5432:5432 -e POSTGRES_PASSWORD=pingtxt postgres:17-alpine
PINGTXT_TEST_DB_URL="postgres://postgres:pingtxt@127.0.0.1:5432/postgres?sslmode=disable" go test ./internal/store/... ./internal/hub/...

docker run -d --name maria -p 3306:3306 -e MARIADB_ROOT_PASSWORD=pingtxt mariadb:11
PINGTXT_TEST_DB_URL="mariadb://root:pingtxt@127.0.0.1:3306/mysql" go test ./internal/store/... ./internal/hub/...
```

A schema change is a new entry at the end of `migrations` in `internal/store/store.go`, with a
`postgres` / `mysql` variant when the SQLite text does not work there. CI runs the tests on SQLite,
PostgreSQL, MariaDB and MySQL.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the dev setup, how to open
issues and how pull requests get reviewed and merged.

- **Bugs**: open a [bug report](https://github.com/federico-it/pingtxt/issues/new?template=bug_report.yml).
- **Ideas**: open a [feature request](https://github.com/federico-it/pingtxt/issues/new?template=feature_request.yml);
  for bigger changes, agree on the approach in an issue before writing code.
- **Security problems**: report them privately as described in [SECURITY.md](SECURITY.md), never in a public issue.

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

Copyright (C) 2026 Federico Mengascini and pingtxt contributors.

pingtxt is free software, licensed under the [GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0).
You can use it for anything, including commercially, self-host it and modify it. If you distribute a modified
version, or run one as a service that other people use over a network, you must publish its source code under
the same license.
