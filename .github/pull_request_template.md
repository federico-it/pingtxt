## What

<!-- What does this PR change, and why? -->

Closes #

## How to test

<!-- Steps a reviewer can follow, or the tests that cover it. -->

## Screenshots

<!-- For UI changes: light and dark, and a phone-sized screen if the layout changed. Delete if not needed. -->

## Checklist

- [ ] `make test lint` passes (and `cd site && npm run typecheck && npm run build` if `site/` or `docs/` changed)
- [ ] Tests added or updated for Go changes
- [ ] Docs in `docs/en` and `docs/it` updated if user-facing behavior changed
- [ ] API changes keep older clients (the iOS app) working: new fields optional, nothing renamed or removed, `internal/hub/openapi.json` updated, and the PR says what changes for the app
- [ ] New database migrations are appended at the end of the list in `internal/store/store.go`
- [ ] No secrets are logged, exported without secrets, or shown to read-only users
- [ ] I have read [CONTRIBUTING.md](https://github.com/federico-it/pingtxt/blob/main/CONTRIBUTING.md) and agree to license my contribution under the project license
