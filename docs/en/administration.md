# Administration

Keeping a hub running: upgrades, backups and restores, moving the configuration between hubs, usage stats,
checking release signatures and the command line flags.

## Upgrade

```sh
docker compose pull && docker compose up -d
```

Database migrations run at start. Agents installed with the script or in Docker follow the hub's version by
themselves, see [agent updates](locations.md#agent-updates). Take a [backup](#backups) first if you want a way back:
a backup from a newer version cannot be restored on an older hub.

While the hub restarts or updates, browsers show a **pingtxt is updating** page that follows the migrations and
reloads by itself when the hub is ready, and `/api/health` answers 503. Open tabs wait on the same screen instead of
failing. When the container is fully down (while pulling the new image), browsers that opened pingtxt before over
https still show that page from their cache; a first visit gets the reverse proxy's error until the hub listens
again.

## Update check

Release builds ask GitHub for the latest pingtxt release every 12 hours. When a newer one is out, admins see a notice
with the release notes and how to update. Turn it off in **settings → general**; for offline installs,
`PINGTXT_DISABLE_UPDATE_CHECK=true` turns it off from the first start, before any request is made. Development and
`main` builds never check.

## Backups

With SQLite, **settings → data → database backups** takes a consistent copy of the whole database (settings, users,
secrets, history, audit log) while the hub keeps running. Turn on **scheduled backups** to take one every N hours
and keep the last N. Files live in `<data>/backups` (`/data/backups` in Docker) and can be downloaded or uploaded
from the same page. A backup is also taken automatically before every configuration import and every restore.

**Restore** replaces the whole database. In the UI pick a backup → *restore*: it is staged and swapped in when the
hub restarts (`docker restart pingtxt`). From a shell, with the hub stopped:

```sh
pingtxt -restore /data/backups/pingtxt-auto-20261030-020000.db
# Docker: docker compose stop pingtxt
#         docker compose run --rm pingtxt -restore /data/backups/<file>.db
#         docker compose start pingtxt
```

The replaced database is kept as `pingtxt-pre-restore-*.db`. Backups from older versions are migrated at start;
backups from a newer version are refused. To take a backup from cron instead: `pingtxt -backup /path/to/file.db`
(safe while the hub runs; in Docker `docker exec -u pingtxt pingtxt pingtxt -backup /data/backups/x.db`).

### PostgreSQL and MariaDB backups

The built-in backups are SQLite files. With PostgreSQL or MariaDB, back up the database server with its own tools:

```sh
# PostgreSQL
docker exec pingtxt-postgres pg_dump -U pingtxt -Fc pingtxt > pingtxt.dump
docker stop pingtxt
docker exec -i pingtxt-postgres pg_restore -U pingtxt -d pingtxt --clean --if-exists < pingtxt.dump
docker start pingtxt
```

```sh
# MariaDB
docker exec pingtxt-mariadb sh -c 'mariadb-dump -u pingtxt -p"$MARIADB_PASSWORD" --single-transaction pingtxt' > pingtxt.sql
docker stop pingtxt
docker exec -i pingtxt-mariadb sh -c 'mariadb -u pingtxt -p"$MARIADB_PASSWORD" pingtxt' < pingtxt.sql
docker start pingtxt
```

## Configuration export and import

**settings → data → configuration export** downloads a JSON file with monitors, groups, tags, notification channels,
maintenance windows, status pages, agents and instance settings: no history and no user accounts. Without
**include secrets**, passwords, bot tokens, secret webhook URLs (Discord, Slack), login client secrets, credential
headers and passwords in target URLs are replaced by `[redacted]`, and agent and heartbeat tokens are left out.

Importing:

- **merge** updates objects with the same name (status pages by slug) and adds the rest; redacted values keep what
  is stored. **replace** deletes the current monitors (with their history), groups, tags, channels, maintenance
  windows and status pages first. Agents are never deleted.
- On another hub, channels with redacted secrets are skipped and monitors with redacted credentials are paused; the
  import result lists what to fill in. Export *with* secrets to move a hub completely: agents keep their tokens and
  heartbeat URLs stay the same. This is also how to move from SQLite to PostgreSQL or MariaDB.

The same is available from the API and the command line:

```sh
curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/config/export > pingtxt.json   # no secrets with a token
pingtxt -export-config pingtxt.json [-export-secrets]
pingtxt -import-config pingtxt.json [-import-mode replace]   # then restart the hub
```

Coming from Uptime Kuma? See [import from Uptime Kuma](monitors.md#import-from-uptime-kuma).

## Usage stats

After installing, admins are asked once whether the hub may send anonymous usage stats. Nothing is sent before
someone says yes, and the answer can be changed any time in **settings → general**, which also shows the exact next
report. Once an hour the hub then sends to `telemetry.pingtxt.com` only:

- `id`: a random id made the first time stats are turned on. It never changes and says nothing about the hub;
- `version`: the pingtxt version;
- `monitors`: how many monitors are not paused;
- `agents`: how many agents are connected.

No names, targets, addresses, results or emails, and the service does not store IP addresses. Start the hub with
`PINGTXT_DISABLE_TELEMETRY=true` to turn the stats off for good and hide the question. Development builds never send.
The totals are public at [telemetry.pingtxt.com](https://telemetry.pingtxt.com/) (JSON at `/v1/stats`): live hubs,
monitors and agents, versions in use and 90 days of history, never a single hub's report. The service is open
source: [pingtxt-telemetry](https://github.com/federico-it/pingtxt-telemetry).

## Verify a release

Releases after v0.1.0 are signed with [cosign](https://docs.sigstore.dev/) in keyless mode: the signature is tied
to the GitHub Actions workflow that built them, with no key to leak. Each release has `SHA256SUMS` and its signature
`SHA256SUMS.sigstore.json`; images on ghcr.io are signed by digest.

```sh
V=v0.2.0   # the release to check
base=https://github.com/federico-it/pingtxt/releases/download/$V
curl -fsSLO $base/SHA256SUMS -O $base/SHA256SUMS.sigstore.json -O $base/pingtxt-agent-linux-amd64

# 1. SHA256SUMS was signed by release.yml for this tag
cosign verify-blob SHA256SUMS --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "https://github.com/federico-it/pingtxt/.github/workflows/release.yml@refs/tags/$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 2. the files you downloaded match it
sha256sum --ignore-missing -c SHA256SUMS
```

```sh
# Docker images (tag without the "v")
cosign verify ghcr.io/federico-it/pingtxt:${V#v} \
  --certificate-identity "https://github.com/federico-it/pingtxt/.github/workflows/docker.yml@refs/tags/$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

`:latest` is the image of the newest release tag, signed like it. `:main` images are signed by `docker.yml@refs/heads/main`.

## Command line

The hub binary has a few flags for things done outside the web UI. In Docker run them as
`docker exec -u pingtxt pingtxt pingtxt <flag>`.

| flag | does |
| --- | --- |
| `-version` | prints the version |
| `-backup <file>` | writes a copy of the database (safe while the hub runs) |
| `-restore <file>` | replaces the database with a backup (stop the hub first) |
| `-export-config <file>` | writes the configuration as JSON (`-` for stdout); add `-export-secrets` to include secrets |
| `-import-config <file>` | applies a configuration file (`-import-mode merge` or `replace`), then restart the hub |
| `-enable-password-login` | turns password login back on |
| `-reset-2fa <email>` | turns two-factor authentication off for a user |
| `-make-admin <email>` | gives a user the admin role |
