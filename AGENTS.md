# Notes for coding agents

Read [CONTRIBUTING.md](CONTRIBUTING.md) first: setup, layout, tests and the pull request checklist are there. These are
the rules that are easy to miss.

## The REST API has clients you cannot see

The iOS app (separate repository, released on its own schedule), API scripts and the MCP server all use `/api`. A user
can run an app version that is months older than the hub, so every API change must keep older clients working:

- **New fields are optional.** A request without them behaves as before, and the default keeps the old behavior.
- **Never rename or remove a field, endpoint or enum value**, and never change a field's type or meaning. Add a new one
  next to it and keep the old one working.
- **`PUT` keeps the fields the body leaves out.** Update handlers read the body with `readJSONOver` (or `readSave` for
  handlers that also create) on top of the stored record, so a client that does not know a newer field cannot reset
  it. Do not go back to decoding a `PUT` body into an empty struct.
- **Document every change in `internal/hub/openapi.json`.** Tests fail when a route is missing
  (`TestOpenAPIMatchesRoutes`) or a JSON field of a mapped Go type is missing (`TestOpenAPISchemasCoverFields`; add new
  types to `openAPISchemaFields`).
- **Say in the pull request what changes for the app**: new fields or endpoints it could use, and anything it must
  send or read differently. "Nothing" is a valid answer.

The same goes for the push relay API (`pingtxt-push-relay`), which the app also calls.

## Other conventions

- Database migrations are an ordered list in `internal/store/store.go`: append at the end, never edit or reorder an
  existing one.
- UI text lives in `web/messages/en.json` and `web/messages/it.json`: change both.
- User-facing behavior is documented in `docs/en` and `docs/it`: change both (the site build checks they match).
- Run `make test lint` before pushing.
