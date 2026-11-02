# Alerts and notifications

Alerts go out through **channels**, added on the **notify** page. Each channel picks which monitors and which
events it gets, and can wait or repeat. **send test** checks a channel before you save it.

## Channels

| type | what you need |
| --- | --- |
| webhook (JSON) | a URL; pingtxt POSTs a JSON body (fields [below](#webhook-payload)) |
| Discord, Slack | the channel's incoming webhook URL |
| Telegram | a bot token and the chat id |
| ntfy | the topic URL, and an access token for protected topics |
| email | an SMTP server (STARTTLS on 587, TLS on 465, or none for a local relay on 25), or the [Resend](https://resend.com) API with a key from a verified domain; then the sender and the recipients |
| pingtxt app (push) | push turned on in **settings → integrations → mobile app**; the users (emails) whose phones get the alerts, empty for everyone. See [iOS app](ios-app.md) |

Every alert says which monitor, what happened and, for down alerts, the cause per location and a link to the
[diagnostics](monitors.md#diagnostics-on-failure). Emails use the same layout as the web UI, in light and dark.

**Desktop notifications**: on the notify page each person can also turn on browser notifications. While a pingtxt
tab is open, even in the background, that browser shows when a monitor goes down or comes back up. Nothing is sent
to anyone else.

## Which monitors and events each channel gets

Each channel has a **scope** and **events**:

- **scope**: *all monitors* (also the ones added later), or the monitors of some **groups** and **tags**, or none
  (only the monitors linked one by one).
- **events**: `down` (with its reminders), `up` (recovery, only to channels that got the alert), `degraded` (some
  locations fail or are slow, and when that ends; off by default), `cert_expiry`, `domain_expiry` and
  `agent_offline` ([agent offline alerts](locations.md#agent-offline-alerts): about agents, so it ignores the
  scope and the monitor links).

A monitor can also be linked to a channel **one by one**, from the channel (its monitor list) or from the monitor
form (*notifications*). The link has events of its own and wins over the scope, so it adds a monitor, changes what
it sends, or leaves the monitor out of an *all monitors* channel.

## Escalation and reminders

Each channel has two optional settings, in minutes:

- **send after**: the channel gets the alert only if the monitor is still down after this time (0 = right away).
  Add a second channel (the on-call phone, a manager's email) with 15 or 30 minutes to escalate long outages: short
  blips never reach it, and it gets the recovery message only if it got the alert.
- **repeat every**: the channel gets a `STILL DOWN` reminder at this interval while the monitor stays down (0 =
  once, at least 5). Webhooks carry `reminder` (1, 2, ...) and `down_since` (unix ms).

Times count from when the incident opened. What each channel received is stored with the incident, so a restart
does not repeat or skip alerts.

## Maintenance windows

On the **maint** page plan windows when alerts should stay quiet: one-off or recurring (daily, weekly, monthly, in
any time zone), for some or all monitors, or whole groups and tags.

During a window the monitors show **maintenance**: checks keep running, but no incidents open, no alerts are sent
and those checks do not count toward uptime. A monitor still down when the window ends gets its alert then.

## Expiry alerts

pingtxt watches the TLS certificate of https monitors and the registration of their domain (RDAP, with WHOIS for
registries without RDAP, such as .it). Turn each one on per monitor. Thresholds in days are set in
**settings → monitors** (default 14/7/3/1 for certificates, 30/14/7/1 for domains): one alert per threshold crossed,
one when it expires and one when it is renewed. Channels get them with the `cert_expiry` and `domain_expiry`
events.

## Email reports

**Periodic reports**, on the notify page, email the uptime, incidents and downtime of the last week (every Monday)
or month (on the 1st), per monitor group, compared with the period before. Pick the email channels, optionally some
groups, the hour and the time zone. **preview** shows a report and **send now** sends one before you turn them on.

## Webhook payload

Webhooks get a JSON body with `monitor_id`, `monitor`, `target`, `status`, `cause` and `at` (unix ms), plus:

| field | when |
| --- | --- |
| `kind` | empty for down and up, `degraded`, `cert_expiry`, `domain_expiry` or `agent` |
| `status` | `down` / `up`, `expiring` / `expired` / `renewed` for expiry events, `offline` / `online` for agents |
| `reminder`, `down_since` | reminders (`down_since` in unix ms) |
| `subject`, `expires_at`, `days_left` | expiry events (host name or domain) |
| `agent_id`, `agent`, `down_since` | agent offline and online alerts |
| `diagnostics`, `diagnostics_url` | the first down alert, when diagnostics were collected |
| `test` | `true` for test notifications |

Use **send test** to see a real payload at your endpoint.
