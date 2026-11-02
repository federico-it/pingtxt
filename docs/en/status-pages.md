# Status pages

Public pages that tell your users what works. Create them on the **status** page of the web UI.

## Create a page

Pick a title, a slug and the monitors, in the order you want. You can also add whole **groups**: every monitor of
the group is shown, including ones added later, and each group is a section with its own status. Choose whether
past incidents are shown. **preview** shows the page as visitors will see it, before saving.

A published page is public at `https://<hub>/status/<slug>/`. Drafts are visible only to logged-in users. Visitors
see monitor names, status, daily uptime history and incident times, never targets, locations or error messages.
The history covers the retention period, up to 90 days.

## Custom domain

To serve a page on its own domain (e.g. `status.example.com`), set it on the page, point a DNS record (CNAME or A)
for the host at the hub and route the host to pingtxt in your [reverse proxy](install.md#reverse-proxy), with its
own TLS certificate. On that host pingtxt serves only the status page, at `/`: the web UI and the API are not
reachable there.

## Password

To keep a page private, turn on **protect with a password**. Visitors then see a password form; the right password
opens the page for 30 days in that browser, together with its badges and subscription form. The password is stored
hashed, wrong attempts are rate limited, and changing it logs every visitor out. Logged-in users never need it.

## Name link and logo

- **name link**: clicking the page name or logo opens this address, for example your website.
- **logo**: SVG, PNG, JPEG, GIF, WebP or AVIF, up to 4 MB, uploaded or imported from a URL. Images are resized and
  served as AVIF and WebP; SVG files are kept as vectors, without scripts.
- **show only the logo**: for logos that already contain the name. The name stays the title of the browser tab.

## Incidents and email subscribers

Open **incidents** on a page (button in the status page list) to tell visitors what is going on: a title, an impact
(none, minor, major, critical) and a timeline of updates, each with a status (*investigating*, *identified*,
*monitoring*, *resolved*) and a message. Open incidents are shown at the top of the page, and their impact makes the
page banner at least *degraded* (minor), *partial outage* (major) or *major outage* (critical). Resolved ones go to
the history.

To let visitors **subscribe by email**, pick an email channel under **email subscriptions** in the page settings:
its SMTP server or Resend account and its sender are used, not its recipients. Visitors enter their address on the
page and confirm it through a link (valid 7 days); then every update posted with **email subscribers** checked
reaches them. Each email has an unsubscribe link and a one-click `List-Unsubscribe` header. Admins see and remove
subscribers under the incidents of the page. Links point to the custom domain of the page, or else to the public URL
from the settings.

## Badges

Each published page has shields-style SVG badges, with no login:

| badge | URL |
| --- | --- |
| state of the page | `/api/badge/<slug>/status.svg` |
| state of a monitor | `/api/badge/<slug>/<monitor id>/status.svg` |
| uptime | `/api/badge/<slug>/<monitor id>/uptime.svg` (default 30 days) |
| response time | `/api/badge/<slug>/<monitor id>/response.svg` (default 24 hours) |

Options: `?period=24h|7d|30d|90d`, `?label=` for the left text, `?style=flat-square` for square corners. Copy the
Markdown from the monitor page or the status page editor:

```markdown
[![uptime](https://status.example.com/api/badge/acme/12/uptime.svg)](https://status.example.com/status/acme/)
```

A badge never shows more than its page: drafts and monitors that are not on the page answer a grey "not found".
