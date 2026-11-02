# Users and security

Who can log in and what they can do: roles, single sign-on, two-factor authentication, the audit log of every
change, and how to get back in when locked out.

## Users and roles

Every user is an **admin** or **read-only**. Admins add users in **settings → access** (with a password, or without
one to log in through single sign-on with the same email) and change roles there.

- **Read-only** users see monitors, incidents, history, agents, maintenance and status pages, but every change is
  refused by the API and the buttons are hidden. Passwords in monitor targets, request headers and bodies, push URLs
  and channel settings are hidden from them. They can still change their own password, 2FA and read tokens.
- Instance settings and users are for admins only. Admins cannot change their own role, so one admin always remains.

## Single sign-on

In **settings → access** turn on GitHub, Google or a generic OpenID Connect provider (Authentik, Keycloak,
Authelia, Pocket ID, Zitadel, Okta, Entra ID and others), paste the client id and secret, and register the callback
URL shown there (`<public url>/api/auth/oauth/<provider>/callback`) at the provider. Set the
[public URL](install.md#configuration) first when the hub is behind a reverse proxy, so the callback is right.

- A provider login is matched to an existing user by verified email, then linked. Users can also connect accounts
  from **settings → account**.
- Turn on *create accounts on first single sign-on* to let new people in, optionally only from some email domains.
  New accounts get the role picked there (read-only by default).
- You can turn off password login once a provider works.

## Two-factor authentication

In **settings → account** scan the QR code with an authenticator app (1Password, Bitwarden, Aegis, Google
Authenticator...) and confirm a code. You get 10 single-use recovery codes. After that, password logins ask for a
code; single sign-on logins rely on the provider's own MFA.

Lost phone and recovery codes? An admin can click **reset 2fa** next to the user in **settings → access**, or see
[locked out](#locked-out).

## Audit log

**settings → audit log** shows who changed what, when and from where: every change made from the web UI, an API
token, an MCP client or the mobile app, plus logins (failed ones too), 2FA changes, tokens and app sign-ins, backups,
imports and the command line recovery flags. Each entry keeps the fields that changed (before → after). Secrets are
never stored: a changed password or token shows as `********`. Changes refused to read-only users and read tokens are
logged as *denied*.

- Only admins see it, filter it and export it (CSV or JSON Lines), from the browser or with an API token
  (`GET /api/audit`, `/api/audit/export`). Monitor and status page pages show their last changes.
- Every user sees their own recent logins, tokens and apps in **settings → account**.
- The client address is the first public IP in the proxy headers (`CF-Connecting-IP`, `X-Forwarded-For`...); the
  address of the connection is kept too, since headers can be forged.
- Entries are kept 365 days (**settings → general**, 30 to 3650) and are part of database backups.

## Locked out

Run these on the server, then restart the hub. In Docker, prefix them with `docker exec -u pingtxt pingtxt`.

| command | does |
| --- | --- |
| `pingtxt -enable-password-login` | turns password login back on, when single sign-on is broken |
| `pingtxt -reset-2fa you@example.com` | turns two-factor authentication off for that user |
| `pingtxt -make-admin you@example.com` | gives that user the admin role |

## Security of the hub

- Login attempts are rate limited (IPv6 clients per /64). Sessions, API tokens and app tokens are stored hashed.
- The containers run as an unprivileged user, see [non-root container](install.md#non-root-container).
- Releases are signed, see [verify a release](administration.md#verify-a-release).
- Report vulnerabilities privately, as described in [SECURITY.md](../../SECURITY.md).
