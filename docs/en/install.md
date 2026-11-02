# Install

The hub is one Docker container with a folder for its data. Agents are optional, see [Locations](locations.md).

## Run the hub

```sh
docker run -d --name pingtxt --restart unless-stopped \
  -p 8090:8090 -v ./data:/data \
  ghcr.io/federico-it/pingtxt:latest
```

Or `docker compose up -d` with the [docker-compose.yml](../../docker-compose.yml) of the repository.
Open `http://localhost:8090`, create the admin account and add your first [monitor](monitors.md).

The image runs on linux/amd64 and linux/arm64. Docker tags: `:latest` is the newest release, `:1.2.3` a given
release, `:main` the latest build of the main branch. Hub binaries for Linux (and agents for Linux and macOS) are on the
[releases page](https://github.com/federico-it/pingtxt/releases), see [without Docker](#without-docker).

## Configuration

Everything is configured from the web UI under **settings** and stored in the database: public URL, built-in
location, history retention, defaults for new monitors, users and login. Only a few environment variables are
read at start:

| env | default | |
| --- | --- | --- |
| `PINGTXT_ADDR` | `:8090` | listen address (IPv4 and IPv6) |
| `PINGTXT_DATA` | `/data` | folder of the SQLite database, backups and staged restores |
| `PINGTXT_DB_URL` | | PostgreSQL or MariaDB/MySQL instead of SQLite, see [below](#postgresql-or-mariadb) |
| `PINGTXT_DISABLE_UPDATE_CHECK` | | `true`: never ask GitHub for new releases, see [administration](administration.md#update-check) |
| `PINGTXT_DISABLE_TELEMETRY` | | `true`: never send usage stats and hide the question, see [administration](administration.md#usage-stats) |
| `PINGTXT_IPV6` | | `on` or `off` skips the IPv6 reachability probe, see [IPv6](#ipv6) |
| `PINGTXT_UNPRIVILEGED_PING` | | `1` or `0` forces the unprivileged or the raw ICMP socket, see [without Docker](#without-docker) |
| `PINGTXT_AGENT_DIR` | | folder of the agent binaries, when they are not next to the hub |

`PINGTXT_PUBLIC_URL`, `PINGTXT_RETENTION_DAYS`, `PINGTXT_LOCAL_NAME` and `PINGTXT_DISABLE_LOCAL` still work, but
only fill in the settings on the first start. After that, change them in the UI.

Set the **public URL** in **settings → general** as soon as the hub has its final address: links in emails,
login callbacks, heartbeat URLs and agent install commands use it.

## Non-root container

The hub and agent images run pingtxt as the user `pingtxt` (uid/gid 10001), not as root. The container starts
as root only for its entrypoint, which:

- gives `/data` to `pingtxt` when some files there belong to another user (volumes of older versions were
  written by root: the first start after the upgrade fixes them);
- adds `pingtxt` to the group of a mounted Docker socket (for [docker container](monitors.md#databases-containers-grpc-and-smtp) monitors);
- drops to `pingtxt` with no capabilities except `CAP_NET_RAW` (for ping), and `no-new-privileges`.

To skip the root step, set the user yourself (`--user 10001:10001`, or `user: "10001:10001"` in compose). The
data folder must then belong to that user (`sudo chown -R 10001:10001 ./data`), and a Docker socket needs
`group_add: ["<gid of /var/run/docker.sock>"]`. If the entrypoint cannot change the owner of `/data` (an NFS
share, for example), it logs a warning and runs as root.

Run command line flags as the same user: `docker exec -u pingtxt pingtxt pingtxt -backup ...`.

## PostgreSQL or MariaDB

SQLite is the default and needs nothing else. To keep the data in **PostgreSQL 13+** or **MariaDB 10.6+ / MySQL
8.0.13+**, set `PINGTXT_DB_URL`. The database must exist and be empty (or hold a pingtxt schema); tables are
created and migrated at start.

```sh
PINGTXT_DB_URL=postgres://pingtxt:secret@db:5432/pingtxt?sslmode=disable
PINGTXT_DB_URL=mariadb://pingtxt:secret@db:3306/pingtxt     # mysql://... works too
```

Special characters in the password must be URL-encoded. The ready-made compose files start the hub and the
database with one command:

```sh
curl -O https://raw.githubusercontent.com/federico-it/pingtxt/main/docker-compose.postgres.yml
echo "DB_PASSWORD=$(openssl rand -hex 16)" > .env
docker compose -f docker-compose.postgres.yml up -d
```

```sh
curl -O https://raw.githubusercontent.com/federico-it/pingtxt/main/docker-compose.mariadb.yml
printf "DB_PASSWORD=%s\nDB_ROOT_PASSWORD=%s\n" $(openssl rand -hex 16) $(openssl rand -hex 16) > .env
docker compose -f docker-compose.mariadb.yml up -d
```

The compose files refuse to start without the passwords in `.env`. The database container uses them only when
it first creates the database, so set them before the first start.

Everything works the same on the three databases, except the built-in
[database backups](administration.md#backups), which are SQLite files: with PostgreSQL or MariaDB back up the
server with its own tools ([commands](administration.md#postgresql-and-mariadb-backups)). To move an existing
SQLite hub, [export the configuration](administration.md#configuration-export-and-import) with secrets and import
it in the new hub; history is not moved.

## Reverse proxy

Put the hub behind a reverse proxy with TLS (Caddy, Traefik, nginx) and set the public URL in
**settings → general**. The WebSocket upgrade must be allowed on `/api/agent/ws`, where agents connect.

```text
status.example.com {
  reverse_proxy 127.0.0.1:8090
}
```

Caddy passes WebSockets with no extra settings. Status pages on a [custom domain](status-pages.md#custom-domain)
are routed to the same hub.

## Coolify

The repository has two compose files ready for [Coolify](https://coolify.io):
[docker-compose.coolify.yml](../../docker-compose.coolify.yml) (SQLite) and
[docker-compose.coolify.postgres.yml](../../docker-compose.coolify.postgres.yml) (PostgreSQL). In Coolify create a
new resource of type **Docker Compose Empty**, paste one of the two files and deploy.

Coolify fills in the variables on its own: `SERVICE_URL_PINGTXT_8090` creates the domain and routes it to port
8090 with TLS, and the PostgreSQL user and password come from `SERVICE_USER_POSTGRES` and
`SERVICE_PASSWORD_POSTGRES`. Nothing has to be written by hand. To use your own domain, change it in the service
settings before the first deploy: the public URL is taken from it only on the first start, then it is changed in
**settings → general**.

The data stays in the `pingtxt-data` volume (and `pingtxt-postgres` for the database). Coolify's Docker network
has no IPv6 by default, so checks to IPv6-only targets fail from the hub; run them from an [agent](locations.md) with IPv6.

## IPv6

The hub listens on IPv4 and IPv6, and every check type works over IPv6 (see
[IPv6 in monitors](monitors.md#ipv6)). Docker gives containers no IPv6 on the default network, so checks to IPv6
targets fail from the container even when the server has IPv6. Enable it on the network (Docker 27 or later):

```yaml
# docker-compose.yml
networks:
  default:
    enable_ipv6: true
```

With `docker run`, create a network with `docker network create --ipv6 pingtxt` and add `--network pingtxt`, or
use `--network host`. Published ports accept IPv6 clients either way.

The hub and every agent test whether they reach the Internet over IPv6 at start and every 10 minutes. Set
`PINGTXT_IPV6=on` or `off` to skip the test on networks that block the addresses it uses.

## Without Docker

Download the hub (`pingtxt-linux-amd64` or `pingtxt-linux-arm64`) and the `pingtxt-agent-*` binaries from the
[releases page](https://github.com/federico-it/pingtxt/releases), or [build them](../../CONTRIBUTING.md#build-from-source).
The hub looks for its data in `./data` and for the agent binaries in `./dist/agents`; set `PINGTXT_DATA` and
`PINGTXT_AGENT_DIR` to put them elsewhere.

**Ping** checks use a raw socket when the process has `CAP_NET_RAW`, and otherwise an unprivileged ICMP socket,
which needs the process group in `net.ipv4.ping_group_range` (most systemd distributions allow it). Give the binary
the capability with `sudo setcap cap_net_raw+ep /usr/local/bin/pingtxt`, or `AmbientCapabilities=CAP_NET_RAW` in
the unit. `PINGTXT_UNPRIVILEGED_PING=1` always uses the unprivileged socket, `0` always the raw one.

A minimal systemd unit:

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

Agents update themselves to the hub's version, so keep the agent binaries of the same release in
`PINGTXT_AGENT_DIR`.
