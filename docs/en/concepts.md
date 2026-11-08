# Concepts

The words used in the rest of the docs, in the order you meet them.

## Hub

The hub is the main program: one Docker container with the API, the scheduler, the database (SQLite by default,
or PostgreSQL / MariaDB) and the web UI. It plans every check, stores the results, opens incidents, sends
alerts and serves status pages. You need exactly one. See [Install](install.md).

## Locations and agents

A **location** is a place checks run from. The hub is a location itself (the built-in one, which you can rename
or turn off). Every **agent** you add is another location: a small program that connects *out* to the hub over
WebSocket, so it needs no open port and works behind NAT. The hub sends it jobs, it sends back results. See
[Locations](locations.md).

## Monitors and checks

A **monitor** is something you watch: a URL, a port, a database, a cron job... It has a type, a target, an
interval and the locations that check it. Each run from one location is a **check**. With **retries** set to N,
a location counts as failing only after N failed checks in a row. See [Monitors](monitors.md).

## Status

Each monitor has one status, decided from all its locations:

| status | when |
| --- | --- |
| **up** | the checks pass |
| **degraded** | some locations fail or are slow, but not more than half. The share is 35% by default (settings → monitors, or per monitor, up to 50%). With one location, being slow is enough |
| **down** | more than half of the online locations fail |
| **pending** | no result yet, or every location of the monitor is an agent that is offline |
| **paused** | you paused it: no checks run |
| **maintenance** | a [maintenance window](alerts.md#maintenance-windows) is on: checks run but raise nothing |

A location is **slow** when its response time is above the monitor's
[slow threshold](monitors.md#degraded-and-slow-threshold), if one is set.

## Incidents

An **incident** opens when a monitor goes down and closes when it is back up. Alerts are sent when it opens and
closes (and as reminders in between, if you want). The incidents page lists them with their causes and, when
[diagnostics](monitors.md#diagnostics-on-failure) are on, what each location saw at the first failure.

Status pages have incidents of their own: written updates you post for your visitors. See
[Status pages](status-pages.md#incidents-and-email-subscribers).

## Uptime

Uptime is the share of time a monitor was not down, over 24 hours, 7, 30 or 90 days. It follows the monitor's
status, not single checks: one failing location among several does not lower it, and degraded counts as up.
Checks taken during maintenance windows do not count.

## Notification channels

A **channel** is where alerts go: email, Slack, Discord, Microsoft Teams, Telegram, Matrix, ntfy,
Gotify, Pushover, PagerDuty, Opsgenie, SMS (Twilio), a webhook or the iOS app. Each channel picks the monitors and
the events it gets. See [Alerts and notifications](alerts.md).

## Groups and tags

A monitor can sit in one **group** and have any number of **tags**. Both filter the dashboard, and status pages,
maintenance windows and channels can pick monitors by group or tag. See
[Monitors](monitors.md#groups-and-tags).

## Using the web UI

- Press <kbd>ctrl k</kbd> to jump to any monitor or page, <kbd>d</kbd> to switch between light and dark.
- Select several monitors on the dashboard to pause, resume, move or delete them together.
- **test** in the monitor form runs the check once from the selected locations, without saving.
- Pin monitors to keep them at the top of the dashboard; pins are kept in your browser.
