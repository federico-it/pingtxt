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
- **footer**: free text and up to 12 links, for example company details, contacts and privacy policy. **hide
  "powered by pingtxt"** removes the credit line.
- **use the logo as the browser tab icon**.

Notice and footer text accept light Markdown: `**bold**`, `*italic*`, `[text](https://…)` and an empty line for a
new paragraph. Links must be `http://`, `https://` or `mailto:` addresses.

**custom CSS** is added to the public page as is, for anything the options do not cover. These classes are stable:
`.pt-page`, `.pt-header`, `.pt-brand`, `.pt-tagline`, `.pt-notice`, `.pt-banner`, `.pt-section`, `.pt-list`,
`.pt-monitor`, `.pt-group`, `.pt-bars`, `.pt-report`, `.pt-subscribe`, `.pt-footer`. Colors are CSS variables
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
