# Locations (agents)

Checks run from **locations**. The hub is one (the built-in location, renamed or turned off in
**settings → general**); each agent you add is another. More locations tell a real outage from a network problem
near one of them, and show response times from where your users are.

## Add an agent

In the UI go to **agents → add agent**: you get a token and ready-to-paste commands.

```sh
# Docker
docker run -d --name pingtxt-agent --restart unless-stopped --cap-add NET_RAW \
  -e PINGTXT_HUB=https://status.example.com -e PINGTXT_TOKEN=pt_xxx \
  -v pingtxt-agent:/data ghcr.io/federico-it/pingtxt-agent:latest
```

```sh
# Linux + systemd (binary downloaded from your hub)
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --hub https://status.example.com --token pt_xxx

# uninstall
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --uninstall
```

The agent is a static binary of about 8 MB for Linux (amd64, arm64, arm) and macOS. It only dials out to the hub
over WebSocket: no open port, works behind NAT, reconnects with backoff and keeps no state. Hubs on IPv6 work too
(`PINGTXT_HUB=http://[2001:db8::1]:8090`).

When a new agent connects, the hub offers to add it to every monitor it can run. Otherwise tick the location in
the monitors you want (**select all** in the locations section ticks every location the monitor can use): the hub plans every check and sends it to the agents.

Give each agent a **country** (in the agent's edit dialog; the hub's own location has it in **settings → general**):
its flag then shows next to the location on the agents page, in the monitor form and on the monitor page.

The agents page shows each agent's version, address, whether it reaches IPv6 and when it was last seen.
**rotate** gives an agent a new token: the old one stops working at once and the agent disconnects.

## How locations vote

With several locations, the monitor is **down** when more than half of the online locations fail, and
**degraded** when at least the degraded share fails or is slow (35% by default). Uptime follows the monitor's
status, so one failing location among several does not lower it. See [Concepts](concepts.md#status).

Each location also has its own response time line on the monitor page, and can have its own
[automatic slow threshold](monitors.md#degraded-and-slow-threshold).

## Agent offline alerts

When an agent stays disconnected longer than the **grace period** (5 minutes by default, set on the agents page,
1 to 1440), channels with the `agent_offline` event get an *offline* alert, and an *online* one when the agent
connects again. Short drops send nothing.

No alert is sent while the agent updates itself (at least 10 minutes of grace), after an admin rotated its token,
when the agent's own **alert when offline** switch is off, or while every monitor it checks is in maintenance (it
goes out when the window ends). Webhooks carry `kind: "agent"`, `status` (`offline` or `online`), `agent_id`,
`agent`, `target` (its host name) and `down_since`.

A monitor that runs only on agents offline past the grace period gets no checks: it turns **pending** (no
incident, no downtime) until an agent reconnects. The dashboard shows a banner with the agents that are offline.

New channels get the event by default. The event is about agents, so it ignores the channel's monitor scope.

## Agent updates

The hub bundles agent binaries built with its own version. When you upgrade the hub, agents (installed with the
script or in Docker) update themselves as soon as they reconnect: the hub sends the download path and the SHA-256
of the binary for the agent's platform, the agent downloads it from the hub, checks the checksum and that the new
binary reports the expected version, swaps the file and restarts in place. A failed update leaves the old binary
running and shows on the agents page; the hub retries after 6 hours, or after a few minutes when the release files
were still being uploaded.

- **agents → update agents automatically** turns this on or off (on by default). Each outdated agent also has an
  **update** button.
- To keep an agent on its version, start it with `-no-auto-update` (or `PINGTXT_NO_AUTO_UPDATE=1` in
  `/etc/pingtxt-agent.env`).
- Agents update themselves only from a hub reached over **https** (or plain http on a loopback or private IP
  address): over plain http anyone on the path could swap the binary and its checksum. To accept that risk, use
  `-insecure-update` (`PINGTXT_INSECURE_UPDATE=1`).
- Agents installed before self-updates existed run as a read-only systemd `DynamicUser`: re-run the install script
  once. It installs to `/opt/pingtxt-agent`, owned by a `pingtxt-agent` user.

### Checksum from GitHub

Before installing, agents compare the hub's checksum with the `SHA256SUMS` of the GitHub release of that version
(the hub bundles the same, reproducibly built binaries). A mismatch, or a release with no checksum for the
platform, refuses the update, so a hub that was tampered with cannot push its own binary. A new hub waits until
its release on GitHub has `SHA256SUMS` (uploaded a few minutes after the Docker image) for up to 30 minutes; with
the [update check](administration.md#update-check) off it updates agents right away.

`-verify-update` (`PINGTXT_VERIFY_UPDATE`) sets how strict this is:

| value | |
| --- | --- |
| `auto` (default) | checks release versions; if GitHub cannot be reached the update goes ahead with a warning in the agent log. Development builds are not checked |
| `strict` | installs only published releases whose checksum matches. Use it when agents can reach github.com |
| `off` | trusts the hub, for forks and private builds (or point `-release-url` / `PINGTXT_RELEASE_URL` at your own release downloads) |

### Docker agents

Docker agents keep the new binary in the `/data` volume (`PINGTXT_UPDATE_DIR`) and run it from there on every
restart, so no Docker socket is needed. Mount a named volume (`-v pingtxt-agent:/data`) to keep updates when the
container is recreated. When you pull a new image, its own binary starts again, the volume is cleared and the hub
updates it if needed.

To keep containers immutable instead (Kubernetes, read-only file system), start the agent with
`PINGTXT_NO_AUTO_UPDATE=1` and let an image updater pull `:latest`, such as
[What's Up Docker](https://github.com/getwud/wud) or the maintained
[Watchtower fork](https://github.com/nicholas-fedor/watchtower). They need the Docker socket, and the agent then
follows the latest release rather than the hub's version.

## Agent settings

| env | flag | |
| --- | --- | --- |
| `PINGTXT_HUB` | `-hub` | URL of the hub |
| `PINGTXT_TOKEN` | `-token` | the agent's token |
| `PINGTXT_NO_AUTO_UPDATE` | `-no-auto-update` | never update itself |
| `PINGTXT_INSECURE_UPDATE` | `-insecure-update` | allow updates from a plain http hub |
| `PINGTXT_VERIFY_UPDATE` | `-verify-update` | `auto`, `strict` or `off`, see above |
| `PINGTXT_RELEASE_URL` | `-release-url` | where release checksums are downloaded from |
| `PINGTXT_UPDATE_DIR` | `-update-dir` | in a container, the volume that keeps self-updates |
| | `-concurrency` | checks run in parallel (32) |
| `PINGTXT_IPV6` | | `on` or `off` skips the IPv6 reachability probe |

The systemd install keeps them in `/etc/pingtxt-agent.env`. Docker checks need the Docker socket on the agent, see
[docker container](monitors.md#databases-containers-grpc-and-smtp).
