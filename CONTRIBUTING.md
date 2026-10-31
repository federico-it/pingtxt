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
[Build from source](README.md#build-from-source).

The public website (pingtxt.com) lives in [`site/`](site/) and is a separate Next.js app:
`cd site && npm ci && npm run dev`.

### Project layout

- `cmd/pingtxt`, `cmd/pingtxt-agent` – hub and agent binaries.
- `internal/check` – probes, shared by hub and agents. A new check type goes here, with tests.
- `internal/hub` – scheduler, status, incidents, notifications, HTTP API, agent WebSocket.
- `internal/store` – SQLite. Migrations are an ordered list in `internal/store/store.go`: **always append**
  a new migration at the end, never edit one that already shipped.
- `internal/expiry` – certificate and domain expiry lookups.
- `web/` – the hub UI (Next.js static export, shadcn/ui, Tailwind 4).
- `site/` – the public website and docs.

## Translating the interface

The hub UI is translated on [Crowdin](https://crowdin.com/project/pingtxt). You don't need to touch the
code: sign in on Crowdin, pick a language and translate. A maintainer merges the pull request that Crowdin
opens with the new translations. Want a language that is not listed? Open an issue and we'll add it.

How it works, for developers:

- All UI text lives in [`web/messages/en.json`](web/messages/en.json), grouped by page or component
  (`dashboard`, `monitorForm`, `settings`, ...). English is the source; the other files
  (`it.json`, `es.json`, ...) are written by Crowdin, so don't edit them by hand except to seed a new
  language.
- Components read text with `useTranslations` from `use-intl`:
  `const t = useTranslations("dashboard")` then `t("title")`. Helpers outside components
  (like `lib/format.ts`) use `tr("time.never")` from `lib/i18n`.
- Messages use ICU syntax: `{name}` for values, `{count, plural, one {# monitor} other {# monitors}}`
  for plurals and `<code>...</code>` tags rendered with `t.rich`. Keep commands, URLs and product names
  out of the strings when you can.
- When you add or change text, update `en.json` only, in the namespace of the page you touched.
  Missing keys in a translation fall back to English. `tsc` fails on a key that doesn't exist in `en.json`.
- [`crowdin.yml`](crowdin.yml) maps `web/messages/en.json` to `web/messages/<lang>.json`. Crowdin uploads
  the source on every push to `main` and opens a pull request with the translations.
- A language shows up in the picker once it is listed in `locales` and `loaders` in
  [`web/lib/i18n.tsx`](web/lib/i18n.tsx). Add it there when its file is reasonably complete.
- The language is chosen per browser (saved in `localStorage`, otherwise taken from the browser
  settings) with the picker in the header, on the login page, on status pages and in **settings**.

## Pull requests

1. Fork the repo and create a branch from `main` (`fix-dns-timeout`, `add-matrix-notifier`...).
2. Keep the PR focused on one thing. Unrelated refactors and formatting changes go in their own PR.
3. Add or update tests for Go changes, and update the [README](README.md) and the site docs
   (`site/app/docs/page.tsx`) when you change behavior users can see.
4. Run `make test lint` (and `cd site && npm run typecheck && npm run build` if you touched `site/`).
5. Open the PR against `main` and fill in the template. Link the issue it closes (`Closes #123`).
   Draft PRs are welcome if you want early feedback.

### Checks and merging

`main` is protected: nobody pushes to it directly, every change goes through a pull request.
A PR can be merged when:

- the required CI checks are green: **go** (`go vet` + `go test`) and **web** (`eslint`, `tsc`, `next build`).
  Changes in `site/` also run the **pages** build;
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
