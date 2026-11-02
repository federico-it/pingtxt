# Contributing to pingtxt

Thanks for helping. Bug reports, fixes, new check types, notification channels, docs and
translations (of the interface and of the docs) are all welcome. This page explains how to open an issue, how to set up
the project and how a pull request gets merged.

By taking part you agree to follow the [code of conduct](CODE_OF_CONDUCT.md).

## Reporting a problem

- **Security issue?** Do not open a public issue. Follow [SECURITY.md](SECURITY.md).
- Search [existing issues](https://github.com/federico-it/pingtxt/issues?q=is%3Aissue) first, including
  closed ones. If you find yours, add a 👍 or the details that are missing instead of a new issue.
- Open a [bug report](https://github.com/federico-it/pingtxt/issues/new?template=bug_report.yml) and fill
  in the form: pingtxt version (shown in **settings** and in `pingtxt -version`), how you run it (Docker,
  binary, agent install script), the steps to reproduce, what you expected and what happened, and the
  relevant logs (`docker logs pingtxt`, `journalctl -u pingtxt-agent`).
- Remove tokens, passwords, hostnames and IPs you don't want public from logs and screenshots.

## Asking for a feature

Open a [feature request](https://github.com/federico-it/pingtxt/issues/new?template=feature_request.yml)
and describe the problem you want to solve before the solution you have in mind. pingtxt tries to stay
small: one container for the hub, tiny agents, plain text UI. Features that fit that, or that many people
need, are the most likely to land.

For anything bigger than a small fix (a new check type, a new notifier, a schema change, a new page),
**open an issue first** and wait for a maintainer to agree on the approach. It saves you writing code that
can't be merged.

## Development setup

You need **Go** (the version in [go.mod](go.mod)) and **Node.js 22** with npm. Docker is optional, for
building the images.

```sh
git clone https://github.com/<you>/pingtxt.git && cd pingtxt
cd web && npm ci && cd ..

make dev-hub   # Go hub on :8090, data in ./data
make dev-web   # Next.js dev server on :3000, proxies /api to :8090
```

Open `http://localhost:3000` and create the admin account. To try an agent locally, add one in
**agents → add agent** and run it with the token you get:

```sh
go run ./cmd/pingtxt-agent -hub http://localhost:8090 -token pt_xxx
```

Other targets:

```sh
make test          # go test ./cmd/... ./internal/...
make lint          # go vet + eslint + tsc
make web hub agents  # production build into ./dist (UI embedded in the hub)
make docker        # build both images locally
```

Production builds, cross-compiling and local Docker images are described in
[Build from source](#build-from-source).

The public website (pingtxt.com) lives in [`site/`](site/) and is a separate Next.js app:
`cd site && npm ci && npm run dev`.

### Project layout

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

- `cmd/pingtxt`, `cmd/pingtxt-agent` – hub and agent binaries.
- `internal/check` – probes, shared by hub and agents. A new check type goes here, with tests.
- `internal/expiry` – certificate and domain (RDAP / WHOIS) expiry lookups, run by the hub.
- `internal/hub` – scheduler, status, incidents, notifications, HTTP API, agent WebSocket.
- `internal/store` – SQLite (pure Go driver `modernc.org/sqlite`, no CGO), PostgreSQL (`lib/pq`) and
  MariaDB/MySQL (`go-sql-driver/mysql`). Queries are written once for SQLite and rewritten for the other two.
  Migrations are an ordered list in `internal/store/store.go`: **always append** a new migration at the end
  (with a `postgres` / `mysql` variant when the SQLite text does not work there), never edit one that already
  shipped.
- `web/` – the hub UI (Next.js 16 static export, shadcn/ui on Base UI, Tailwind 4).
- `site/` – the public website; it renders the docs from `docs/`.
- `docs/` – the user docs in Markdown, English and Italian.

**Why Go for the backend:** the agent must be a tiny static binary that runs anywhere (amd64/arm64/arm,
Linux/macOS). The same language lets the hub and the agents share the probe code and the wire protocol, Go's
concurrency fits thousands of periodic checks, and the hub compiles to one binary with the UI embedded, so the
container needs no Node runtime.

### Tests on PostgreSQL and MariaDB

The store and hub tests run on SQLite by default. To run them on a database server (each test creates and drops
its own scratch database, so the user needs `CREATE DATABASE`):

```sh
docker run -d --name pg -p 5432:5432 -e POSTGRES_PASSWORD=pingtxt postgres:17-alpine
PINGTXT_TEST_DB_URL="postgres://postgres:pingtxt@127.0.0.1:5432/postgres?sslmode=disable" go test ./internal/store/... ./internal/hub/...

docker run -d --name maria -p 3306:3306 -e MARIADB_ROOT_PASSWORD=pingtxt mariadb:11
PINGTXT_TEST_DB_URL="mariadb://root:pingtxt@127.0.0.1:3306/mysql" go test ./internal/store/... ./internal/hub/...
```

CI runs the tests on SQLite, PostgreSQL, MariaDB and MySQL.

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
Agents update themselves to the hub version, so build hub and agents from the same checkout. To run the binaries
(data folder, ping permissions, a systemd unit), see [Install without Docker](docs/en/install.md#without-docker).

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
PostgreSQL and MariaDB (create the `.env` with the passwords first):

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.postgres.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.mariadb.yml -f docker-compose.build.yml up -d --build
```

Pushing a `v*` tag publishes a GitHub release with the hub and agent binaries and `SHA256SUMS`, plus the Docker
images for that tag, which also become `:latest` (pre-releases excepted). Merges on main only update the `:main`
images.

## Writing docs

The user docs are Markdown files in [`docs/`](docs/): `docs/en/<page>.md` and the same page in
`docs/it/<page>.md`. GitHub shows them as they are, and pingtxt.com/docs renders them (the site reads them at
build time, see [`site/lib/docs.ts`](site/lib/docs.ts)).

- Change the English and the Italian page together. Both must have the same headings in the same order: the site
  build fails otherwise. If you don't write Italian, copy the English text into the Italian page and say so in
  the PR.
- Link other pages with relative links (`[alerts](alerts.md#escalation-and-reminders)`), files of the repository
  with `../../path`. Anchors follow GitHub's rules (lower case, punctuation dropped, spaces to `-`), and the site
  build fails on a link to a page or anchor that does not exist.
- Write for someone running pingtxt, not for someone reading the code: what to click, what to set, what happens.
  Name settings as the UI does (**settings → monitors**).
- A new page needs its file in both languages and an entry in `DOC_PAGES` in `site/lib/docs.ts`, plus a line in
  `docs/en/README.md` and `docs/it/README.md`.
- Check the result with `cd site && npm run build` (or `npm run dev`).

## Translating the interface

Translations live in the repository, in [`web/messages`](web/messages): one JSON file per language, next
to the English source `en.json`. To fix a wrong word or fill in a missing string, edit the file of your
language and open a pull request. Want a language that is not listed? Copy the keys you can translate into
a new `<code>.json` file and open a pull request (or an issue if you'd rather ask first).

How it works, for developers:

- All UI text lives in [`web/messages/en.json`](web/messages/en.json), grouped by page or component
  (`dashboard`, `monitorForm`, `settings`, ...). English is the source; the other files
  (`it.json`, ...) hold the translations and are edited by hand.
- Components read text with `useTranslations` from `use-intl`:
  `const t = useTranslations("dashboard")` then `t("title")`. Helpers outside components
  (like `lib/format.ts`) use `tr("time.never")` from `lib/i18n`.
- Messages use ICU syntax: `{name}` for values, `{count, plural, one {# monitor} other {# monitors}}`
  for plurals and `<code>...</code>` tags rendered with `t.rich`. Keep commands, URLs and product names
  out of the strings when you can.
- When you add or change text, update `en.json` in the namespace of the page you touched, and add the
  same key to `it.json` (and any other language you can write). Missing keys in a translation fall back
  to English, so a language you can't write is fine to leave out. `tsc` fails on a key that doesn't
  exist in `en.json`.
- A language shows up in the picker once it is listed in `locales` and `loaders` in
  [`web/lib/i18n.tsx`](web/lib/i18n.tsx). Add it there when its file is reasonably complete.
- The language is chosen per browser (saved in `localStorage`, otherwise taken from the browser
  settings) with the picker in the header, on the login page, on status pages and in **settings**.

## Pull requests

1. Fork the repo and create a branch from `main` (`fix-dns-timeout`, `add-matrix-notifier`...).
2. Keep the PR focused on one thing. Unrelated refactors and formatting changes go in their own PR.
3. Add or update tests for Go changes, and update the docs in [`docs/`](docs/) (English and Italian, see
   [Writing docs](#writing-docs)) when you change behavior users can see.
4. Run `make test lint` (and `cd site && npm run typecheck && npm run build` if you touched `site/` or `docs/`).
5. Open the PR against `main` and fill in the template. Link the issue it closes (`Closes #123`).
   Draft PRs are welcome if you want early feedback.

### Checks and merging

`main` is protected: nobody pushes to it directly, every change goes through a pull request.
A PR can be merged when:

- the required CI checks are green: **go** (`go vet` + `go test`) and **web** (`eslint`, `tsc`, `next build`).
  Changes in `site/` or `docs/` also run the **pages** build;
- a maintainer has reviewed and approved it;
- the branch is up to date with `main` and has no conflicts (use **Update branch** or rebase).

A maintainer merges the PR. If a review asks for changes, push new commits to the same branch; there's
no need to open a new PR or force-push. PRs with no activity for 30 days after a review may be closed,
and you can reopen them any time.

### Commit messages

Look at `git log` for the style:

- Short summary in the imperative mood, capitalized, no trailing period, about 50–72 characters:
  `Add Matrix notifier`, `Fix DNS timeout on agents`, `Hide DSN passwords in config exports`.
- If needed, a blank line and a body that explains *why*, wrapped at 72 characters.
- One logical change per commit. Squash "fix typo" commits before the review, or let the maintainer
  squash on merge.

### Code style

- Go: `gofmt`, `go vet` clean, standard library first. Keep the hub a single static binary with no CGO.
- TypeScript: the existing ESLint and TypeScript config, components from `web/components/ui`.
- UI text goes in `web/messages/en.json` (see [Translating the interface](#translating-the-interface)) and
  is lowercase, plain and short, like the rest of the interface. Check light and dark mode
  and a phone-sized screen.
- Never log or return secrets (passwords, tokens, webhook URLs) to read-only users or in exports
  without secrets.

## License

pingtxt is licensed under the [GNU Affero General Public License v3.0](LICENSE). By contributing you
agree that your contributions are licensed under the same license.
