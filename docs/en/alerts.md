# Alerts and notifications

Alerts go out through **channels**, added on the **notify** page. Each channel picks which monitors and which
events it gets, and can wait or repeat. **send test** checks a channel before you save it.

## Channels

| type | what you need |
| --- | --- |
| webhook (JSON) | a URL; pingtxt POSTs a JSON body (fields [below](#webhook-payload)) |
| Discord, Slack | the channel's incoming webhook URL |
| Telegram | a bot token and the chat id |
| Microsoft Teams | the URL of a Teams workflow: in the channel, **⋯ → Workflows → Post to a channel when a webhook request is received**. Old connector URLs (`webhook.office.com`) work too. Alerts are Adaptive Cards |
| Matrix | the homeserver URL, the access token of a bot account and the room (`!id:server` or `#alias:server`). Invite the bot to the room and accept the invite first |
| ntfy | the topic URL, and an access token for protected topics |
| Gotify | the server URL and the token of a Gotify application. Problems have priority 8 (the Android app rings), degraded 5, recoveries 2 |
| Pushover | your user (or group) key and the token of a Pushover application; optionally one device. Problems are sent with high priority |
| PagerDuty | the Events API v2 integration key of a service, and the region (US or EU). A down opens an incident, its reminders join it and the recovery resolves it; the same for degraded, expiry and agent alerts. With the `ack` event, acknowledging in pingtxt acknowledges the PagerDuty incident |
| Opsgenie | the key of an API integration, and the region (US or EU). Alerts get P1 (down, agent offline), P2 (expired) or P3 (expiring, degraded) and close by themselves on recovery. With the `ack` event, acknowledging in pingtxt acknowledges the alert |
| SMS (Twilio) | the account SID and auth token, the sender (a Twilio number or a messaging service SID `MG...`) and the numbers in international format (`+39...`). Each alert is one short SMS per number, paid on your Twilio account |
| email | an SMTP server (STARTTLS on 587, TLS on 465, or none for a local relay on 25), or the [Resend](https://resend.com) API with a key from a verified domain; then the sender and the recipients |
| pingtxt app (push) | push turned on in **settings → integrations → mobile app**; the users (emails) whose phones get the alerts, empty for everyone. See [iOS app](ios-app.md) |

Links in alerts (Teams, Pushover, Gotify, Matrix, PagerDuty, Opsgenie) point to the diagnostics, or to the monitor
when the public URL is set in **settings**. A test sent to PagerDuty or Opsgenie opens an incident and resolves it
right away.

Every alert says which monitor, what happened and, for down alerts, the cause per location and a link to the
[diagnostics](monitors.md#diagnostics-on-failure). Emails use the same layout as the web UI, in light and dark.

**Desktop notifications**: on the notify page each person can also turn on browser notifications. While a pingtxt
tab is open, even in the background, that browser shows when a monitor goes down or comes back up. Nothing is sent
to anyone else.

## Which monitors and events each channel gets

Each channel has a **scope** and **events**:

- **scope**: *all monitors* (also the ones added later), or the monitors of some **groups** and **tags**, or none
  (only the monitors linked one by one).
- **events**: `down` (with its reminders), `up` (recovery, only to channels that got the alert), `ack` (someone
  [acknowledged the incident](#acknowledging-incidents), only to channels that got the alert; off by default), `degraded` (some
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

## Acknowledging incidents

When someone takes an open incident, everyone else should know and stop being paged. On the monitor page, the
incidents page or from a notification of the iOS app, an admin presses **acknowledge**:

- the incident shows **who is on it** and since when, on the dashboard, the monitor and the incidents page;
- its `STILL DOWN` reminders stop, and so do the alerts of channels still waiting for their **send after** time
  (escalation);
- channels that got the down alert and have the `ack` event get a message with who acknowledged it. On the iOS app
  it replaces the down notification.

The recovery still goes to every channel that got the alert. **release** takes the acknowledgement back: reminders
and escalation run again from where they stopped. Another admin can **take over** an incident someone else
acknowledged. Read-only users see who is on it but cannot acknowledge. API: `POST` and `DELETE
/api/incidents/{id}/ack`; acknowledgements are in the [audit log](security.md#audit-log).

## Maintenance windows

On the **maint** page plan windows when alerts should stay quiet: one-off or recurring (daily, weekly, monthly, in
any time zone), for some or all monitors, or whole groups and tags.

During a window the monitors show **maintenance**: checks keep running, but no incidents open, no alerts are sent
and those checks do not count toward uptime. A monitor still down when the window ends gets its alert then.

A window can also be announced on status pages and emailed to their subscribers: see
[scheduled maintenance](status-pages.md#scheduled-maintenance).

## Dependencies

When many monitors run on the same server, tell each of them what it depends on: in the monitor form, under
**dependencies**, add the monitor of the server (a ping, for example). If the server goes down you get one alert for
the server, not one for each service on it.

- While a monitor it depends on is down, a failing monitor is **blocked by** that monitor: it keeps checking and shows
  its own status (down, so uptime and status pages stay accurate), but it opens no incident and sends no alerts. The
  dashboard and the monitor page say which monitor blocks it. Dependencies chain: if the server blocks the site and
  the site blocks the app, the app is blocked by the server.
- When the server is back, each blocked monitor is checked again right away, and only those that still fail alert:
  that is a different problem.
- Checks of different monitors do not run at the same moment. When a service fails first, its alert waits for a fresh
  check of the server (taken right away, and its retries, up to 10 minutes): if the server is down too, the service
  stays quiet.

A monitor can depend on up to 20 monitors, and the hub refuses loops. In the API this is `depends_on` (monitor ids),
and `blocked_by` on the monitor lists while it is blocked.

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
| `kind` | empty for down and up, `ack`, `degraded`, `cert_expiry`, `domain_expiry` or `agent` |
| `status` | `down` / `up`, `acknowledged`, `expiring` / `expired` / `renewed` for expiry events, `offline` / `online` for agents |
| `reminder`, `down_since` | reminders (`down_since` in unix ms) |
| `incident_id` | down, up and acknowledgement events |
| `ack_by` | acknowledgements: email of who acknowledged the incident |
| `subject`, `expires_at`, `days_left` | expiry events (host name or domain) |
| `agent_id`, `agent`, `down_since` | agent offline and online alerts |
| `diagnostics`, `diagnostics_url` | the first down alert, when diagnostics were collected |
| `test` | `true` for test notifications |

Use **send test** to see a real payload at your endpoint.
