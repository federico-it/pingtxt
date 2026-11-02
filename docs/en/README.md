# pingtxt docs

pingtxt is a self-hosted uptime monitor. One container runs the **hub** (API, scheduler, database and web UI);
small **agents** check your services from more places. Almost everything is configured from the web UI.

## Start here

1. [Install the hub](install.md) with Docker and create the admin account.
2. [Add a location](locations.md) if you want checks from more than one place (optional).
3. [Create your first monitor](monitors.md).
4. [Add a notification channel](alerts.md) so you hear about outages.
5. [Publish a status page](status-pages.md) for your users (optional).

New to pingtxt? [Concepts](concepts.md) explains hubs, locations, statuses and incidents in two minutes.

## All pages

| page | what is in it |
| --- | --- |
| [Concepts](concepts.md) | hub, locations, monitors, statuses (up, degraded, down...), incidents, uptime |
| [Install](install.md) | Docker, configuration, PostgreSQL or MariaDB, reverse proxy, IPv6, running without Docker |
| [Locations (agents)](locations.md) | add an agent, how locations vote, offline alerts, agent updates |
| [Monitors](monitors.md) | every check type, JSON rules, slow threshold, IPv6, groups and tags, diagnostics |
| [Alerts and notifications](alerts.md) | channels, which events each channel gets, escalation, maintenance, expiry alerts, reports |
| [Status pages](status-pages.md) | public pages, custom domain, password, logo, incidents and subscribers, badges |
| [iOS app](ios-app.md) | sign in, push notifications and their actions, Live Activity |
| [Users and security](security.md) | roles, single sign-on, two-factor authentication, audit log |
| [Integrations](integrations.md) | API tokens and REST API, MCP server for AI assistants, Prometheus |
| [Administration](administration.md) | upgrades, backups, configuration export and import, usage stats, verifying releases |

The [REST API reference](https://pingtxt.com/docs/api/) lists every endpoint. To work on pingtxt itself, see
[CONTRIBUTING.md](../../CONTRIBUTING.md).

These pages are also on [pingtxt.com/docs](https://pingtxt.com/docs/), in English and
[Italian](https://pingtxt.com/it/docs/). The source is the `docs/` folder of the repository: fix a page there and
both the site and GitHub show the change.
