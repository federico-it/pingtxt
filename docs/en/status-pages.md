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
Feed readers use [private links](#rss-and-atom-feeds) instead.

## Name link and logo

- **name link**: clicking the page name or logo opens this address, for example your website.
- **logo**: SVG, PNG, JPEG, GIF, WebP or AVIF, up to 4 MB, uploaded or imported from a URL. Images are resized and
  served as AVIF and WebP; SVG files are kept as vectors, without scripts.
- **show only the logo**: for logos that already contain the name. The name stays the title of the browser tab.

## Look

The **look** section changes how the page looks; the preview shows every change before saving.

- **preset**: a ready-made look (pingtxt, clean, corporate, soft, editorial, high contrast). It sets colors, font
  and shape and keeps your texts and links. Change anything afterwards.
- **theme**: the visitor's choice (with the light/dark button) or always light or always dark.
- **colors**: background, surfaces, text, secondary text, borders, primary color and the three status colors, for
  the light and the dark theme. Empty colors keep the default. Text on the primary color turns black or white by
  itself, and the editor shows the WCAG contrast of the text colors. **color-blind safe status colors** swaps green
  and red for blue and orange.
- **font**: JetBrains Mono, IBM Plex Sans, Figtree, Atkinson Hyperlegible, Source Serif 4 or the visitor's system
  font, in three sizes. Fonts are served by the hub, never by a third party.
- **corners, lines, density, width, uptime bars**: sharp or rounded corners (0-24 px), dashed or solid lines or
  cards, compact or airy spacing, narrow or wide page, block, rounded or thin bars.
- **header**: plain or a band in the primary color, left or centered, a subtitle and up to 6 links.
- **notice**: a highlighted message above the status, for example planned work.
- **footer**: free text and up to 12 links, for example company details, contacts and privacy policy. The credit line
  "powered by pingtxt" links to pingtxt.com; **hide "powered by pingtxt"** removes it.
- **use the logo as the browser tab icon**.

Notice and footer text accept light Markdown: `**bold**`, `*italic*`, `[text](https://…)` and an empty line for a
new paragraph. Links must be `http://`, `https://` or `mailto:` addresses.

**custom CSS** is added to the public page as is, for anything the options do not cover. These classes are stable:
`.pt-page`, `.pt-header`, `.pt-brand`, `.pt-tagline`, `.pt-notice`, `.pt-banner`, `.pt-section`, `.pt-list`,
`.pt-monitor`, `.pt-group`, `.pt-bars`, `.pt-report`, `.pt-maintenance`, `.pt-subscribe`, `.pt-footer`. Colors are CSS variables
(`--background`, `--foreground`, `--primary`, `--muted`, `--border`, `--up`, `--warn`, `--down`). Images or fonts
the CSS loads from other sites are fetched by every visitor, so they see the visitor's address. The password form
of a protected page uses the same colors, font and CSS, without texts and links.

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

## Scheduled maintenance

Turn on **show on status pages** in a [maintenance window](alerts.md#maintenance-windows) to announce it on every
page that shows one of its monitors. Its name is the title and the **public message** is shown under it; the notes
stay private. Upcoming windows are listed under **scheduled maintenance** with their start, end and the monitors of
the page they cover (recurring windows show their next occurrence); while a window is running it sits at the top of
the page. A window does not change the page banner: its monitors show *maintenance*.

With **email the subscribers** on, the subscribers of those pages get an email when the window is saved or its
schedule changes, optionally a **reminder** before each occurrence (15 minutes to 7 days), when it starts, when it
ends and when it is cancelled (deleted, disabled or no longer public). Times are in the time zone of the window, with
UTC next to them. Emails sent by the scheduler (reminder, start, end) link to the custom domain of the page or to the
public URL from the settings: without either they are skipped. A start or end found more than 15 minutes late, for
example after a restart, is not emailed.

The [webhooks](#webhooks) of those pages get the same news as `maintenance.*` events, also when the window does not
email subscribers.

## RSS and Atom feeds

Every published page has an Atom and an RSS feed, so feed readers (or the RSS apps of Slack and Teams) can follow it:

| feed | on the hub | on a custom domain |
| --- | --- | --- |
| Atom | `/api/public/status/<slug>/feed.atom` | `/api/public/status/feed.atom` |
| RSS 2.0 | `/api/public/status/<slug>/feed.rss` | `/api/public/status/feed.rss` |

Each incident written on the page is one entry with its whole timeline, the latest change first; when the page shows
the incident history, the outages of its monitors are entries too. The public page links the feed in its footer and
announces it to browsers. The page editor shows both addresses under **rss and atom feeds**.

A feed follows the rules of its page: drafts have none, and a password protected page needs the password. Feed
readers cannot type one, so the editor of a protected page shows **private links** with a key: anyone with the link
can read the feed, and changing the password changes the links.

## Webhooks

Under **webhooks** in the page editor, admins add addresses that get the events of the page as JSON `POST`
requests, up to 10 per page. Pick the events or keep **every event**:

| event | sent when |
| --- | --- |
| `incident.created` | an incident is opened on the page |
| `incident.updated` | an incident gets an update |
| `incident.resolved` | an incident gets a *resolved* update |
| `page.status_changed` | the overall status of the page changes (`operational`, `degraded`, `partial_outage`, `major_outage`) |
| `monitor.status_changed` | a monitor shown on the page changes status |
| `maintenance.scheduled` | a [public maintenance window](#scheduled-maintenance) covering a monitor of the page is announced or rescheduled |
| `maintenance.reminder` | its reminder is due (when the window has one) |
| `maintenance.in_progress` | an occurrence starts |
| `maintenance.completed` | an occurrence ends |
| `maintenance.cancelled` | it is deleted, disabled or no longer public before it ends |

Incident events follow the box that notifies subscribers (**send to webhooks** when the page has no email
subscriptions): an update posted with it unchecked stays quiet. Status changes are
checked every 30 seconds. Only published pages send events; **test** sends a `ping` event to one webhook at any
time.

The body always has `event`, `id` (of the delivery), `at` (Unix time in milliseconds) and `page` (`id`, `slug`,
`title`, `url`), then the data of the event: `incident` and `update`; `status` and `previous_status`; `monitor`
(`id`, `name`, `group`, `status`, `previous_status`); or `maintenance` (`id`, `title`, `description`, `status`,
`starts_at`, `ends_at`, `kind`, `monitor_ids`, like the public page).

```json
{
  "event": "incident.resolved",
  "id": "4f1c2a9e0b7d35c8e6a1f203",
  "at": 1794150730633,
  "page": { "id": 1, "slug": "acme", "title": "Acme", "url": "https://status.example.com/" },
  "incident": { "id": 7, "title": "Slow logins", "impact": "major", "status": "resolved", "...": "..." },
  "update": { "id": 19, "status": "resolved", "message": "Fixed.", "created_at": 1794150730600 }
}
```

Every request carries `X-Pingtxt-Event` and `X-Pingtxt-Signature: t=<Unix seconds>,v1=<signature>`, where the
signature is the hex HMAC-SHA256 of `<t>.<body>` with the secret of the webhook (shown in the editor, where you can
also make a new one). Check it, and reject old `t` values, before trusting a request. An answer outside 2xx counts
as a failure: the hub tries again after 10 seconds and after a minute (not for 4xx answers other than 408 and 429),
and the editor shows the result of the latest delivery. Each webhook gets its events one at a time, in order.

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
