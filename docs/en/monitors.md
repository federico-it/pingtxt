# Monitors

A monitor checks one thing at an interval, from the [locations](locations.md) you pick. Create one with
**new monitor** on the dashboard; **test** runs the check once from the selected locations without saving.

## Common settings

| setting | |
| --- | --- |
| interval | seconds between checks (defaults for new monitors are in **settings → monitors**) |
| timeout | ms before a check fails |
| retries | consecutive failures before a location counts as failing (1 to 10) |
| locations | the hub and the agents that run the check |
| group, tags | see [groups and tags](#groups-and-tags) |
| notifications | channels linked to this monitor one by one, see [alerts](alerts.md#which-monitors-and-events-each-channel-gets) |
| degraded threshold | share of failing locations that makes it degraded, see [below](#degraded-and-slow-threshold) |
| slow threshold | off, automatic or a value in ms |
| ip version | how to treat hosts with IPv4 and IPv6, see [IPv6](#ipv6) |
| diagnostics | instance default, on or off, see [diagnostics](#diagnostics-on-failure) |
| expiry alerts | TLS certificate and domain registration, see [alerts](alerts.md#expiry-alerts) |

Paste a `curl` command in the URL field of an HTTP monitor to fill in the method, headers and body.

## Check types

| type | target | what is checked |
| --- | --- | --- |
| http(s) | `https://example.com/health` | status code (default 200-399), headers, body, [JSON rules](#response-checks-json), TLS expiry |
| keyword | URL + text | the response body contains (or does not contain) the text |
| tcp port | `host:port` | the connection opens |
| ping | host or IP | ICMP echo, or ICMPv6 |
| dns | name + record type | the name resolves, optionally to an expected value, through the system or a given server |
| postgresql | `postgres://user:pass@host:5432/db?sslmode=disable` | connect and run a query (default `SELECT 1`); optional expected value of the first column |
| mysql / mariadb | `mysql://user:pass@host:3306/db` (`?tls=true\|false\|skip-verify`) | same as postgresql |
| redis | `redis://[user]:pass@host:6379/0`, `rediss://` for TLS | `AUTH` + `PING` |
| mongodb | `mongodb://user:pass@host:27017/?authSource=admin`, `mongodb+srv://` | `hello`, SCRAM-SHA-256/SHA-1 auth, `ping`; shows primary/secondary |
| docker container | container name or id | down when not running, restarting, paused or `unhealthy` |
| grpc health | `grpc://host:port[/service]`, `grpcs://` for TLS | `grpc.health.v1.Health/Check` returns `SERVING` |
| smtp | `smtp://host:25`, `smtp://host:587`, `smtps://host:465` | greeting, `EHLO`, `STARTTLS` when offered (certificate verified) |
| mcp server | `https://mcp.example.com/mcp` | the MCP handshake and its tools, see [MCP servers](#mcp-servers) |
| heartbeat (push) | none: pingtxt gives you a URL | your job calls the URL; down when the calls stop, see [heartbeat](#heartbeat-monitors-cron-jobs) |

HTTP monitors also take a method, request headers and body, the accepted status codes and whether to skip TLS
verification. Agents older than a check type report `unknown monitor type`: update them.

Ping monitors send **3 echo requests** per check by default, 300 ms apart; set **packets per check** from 1 to 20
(API `ping_count`). The check is up when at least one reply comes back, and the result shows how many did, e.g.
`9/10 packets, 10% loss`. The timeout covers the whole run, so it must be at least (packets - 1) x 300 + 500 ms.
Agents older than this setting send 3 packets: update them.

To be stricter, set the **packet loss limit** (API `ping_loss_pct`): the check fails when at least that share of the
packets is lost, e.g. 50 fails `5/10 packets, 50% loss`. 0 (default) fails only when every packet is lost. Agents
older than this setting fail the check with "update it" instead of ignoring the limit.

## Response checks (JSON)

HTTP and keyword monitors can check the JSON body of the response, for example a health endpoint that returns
`{"status": "ready", "checks": {"redis": {"status": "ok", "latencyMs": 3}}}`. Add rules under
**response checks**: all must pass, otherwise the check fails and the result, the incident and the alert say which
rule broke, e.g. `checks.redis.status: expected "ok", got "error"`.

| path | operator | value |
| --- | --- | --- |
| `status` | equals | `ready` |
| `checks.redis.status` | equals | `ok` |
| `checks.worker.ageMs` | < | `60000` |
| `items[0].id` | exists | |

- Paths use dots and brackets: `a.b`, `items[0]` (or `items.0`, `items[-1]` for the last one),
  `["key.with.dots"]`. A leading `$.` is allowed.
- Operators: equals, not equals, contains (substring, array element or object key), not contains, exists, not
  exists, `>`, `>=`, `<`, `<=` (numbers) and matches (regular expression).
- Values are compared by type: numbers as numbers (`3` equals `3.0`), `true` / `false` / `null` by name, objects
  and arrays as JSON.
- Rules run on agents too. Agents older than this feature fail the check with "update it" instead of passing it
  unchecked.

## Response time per phase

HTTP and keyword checks measure where the time goes: **DNS** lookup, **connect**, **TLS** handshake, **wait** for
the first byte (the server at work) and **transfer** of the body. On the monitor page switch the response time
chart to **by phase**, per location or all together; hover a latency in the check log for the breakdown of that
check. With redirects the phases of every hop are added up.

The monitor page also shows uptime and response time percentiles (p50, p95, p99) for any period, compared with the
period before. Drag on the chart to zoom in.

## Degraded and slow threshold

A monitor is **degraded** when some of its locations fail, but not more than half. The share that counts is set in
**settings → monitors** (35% by default) and can be changed per monitor, up to 50%. Degraded opens no incident and
counts as up for uptime; channels can get it as an event (off by default).

A monitor can also turn degraded when it answers **too slowly**. Set the **slow threshold**:

- **off** (default);
- **fixed**, in ms (below the timeout): a location is slow when the median of its last 3 successful checks is above
  it, so one spike changes nothing;
- **automatic**: each location gets its own threshold, 3 times its usual response time and at least 100 ms more.
  The usual time is the median of its hourly averages over the last 7 days, so a far-away agent has its own
  baseline. It starts once a location has 24 hours of history.

Slow locations count like failing ones toward the degraded share, never toward down. With one location, being
slow is enough. The response time chart draws the threshold as a dashed line, and the alert says which location
was slow and by how much. For heartbeat monitors the threshold applies to the `ping=` duration.

## IPv6

Every check type works over IPv6, and hosts with only AAAA records just work. Put IPv6 addresses in brackets when a
port follows: `[2001:db8::1]:443` (tcp), `https://[2001:db8::1]/`, `redis://[2001:db8::1]:6379`,
`grpc://[2001:db8::1]:50051`. Ping and dns take the bare address (`2001:db8::1`); ping uses ICMPv6.

**ip version** sets how a check treats a host with both IPv4 and IPv6:

| option | API `ip_family` | the check |
| --- | --- | --- |
| instance default | `0` | uses the default set in **settings → monitors** |
| IPv4 or IPv6 | `1` | one connection, whichever family answers first: a broken IPv6 goes unnoticed |
| IPv4 only / IPv6 only | `4` / `6` | only that family |
| IPv4 and IPv6 | `46` | both, in parallel: fails when either fails (the message says which), latency is the slower one |

The hub and every agent test whether they reach the Internet over IPv6; the agents page and the monitor form show
the result. A location without IPv6 cannot be chosen for *IPv6 only* or *IPv4 and IPv6*; *IPv4 or IPv6* and the
instance default check over IPv4 there. TLS certificate expiry is read over either family. For Docker, see
[IPv6 in Docker](install.md#ipv6).

## Databases, containers, gRPC and SMTP

Passwords in connection URLs are hidden in the monitor list, from read-only users and in alerts; use a read-only
database user. PostgreSQL, MySQL and MongoDB use TLS when the server offers it.

Docker checks talk to the Docker Engine of the location that runs them: the hub or the agent needs
`/var/run/docker.sock` (or `DOCKER_HOST`). With Docker, add `-v /var/run/docker.sock:/var/run/docker.sock:ro` to
the hub or agent container (the entrypoint lets the `pingtxt` user use it). For an agent installed with systemd,
allow it with `sudo systemctl edit pingtxt-agent`:

```ini
[Service]
SupplementaryGroups=docker
```

Access to the Docker socket is equivalent to root on that machine (`:ro` does not limit the API). pingtxt only
reads `GET /containers/<name>/json`, but enable it only where you need container checks.

## MCP servers

A monitor of type **mcp server** checks a remote [Model Context Protocol](https://modelcontextprotocol.io) server.
(To let AI assistants use pingtxt itself, see the [MCP server of the hub](integrations.md#mcp-server).)

| setting | |
| --- | --- |
| target | URL of the endpoint, e.g. `https://mcp.example.com/mcp` |
| transport | `auto` (default): streamable HTTP, falling back to the legacy HTTP+SSE transport (2024-11-05) when the POST is rejected with 400/404/405/406/415; or force `streamable` / `sse` |
| headers | sent with every request, e.g. `Authorization: Bearer <token>`; a 401 or 403 makes the monitor go down. Hidden from read-only users |
| required tools | optional, comma-separated: down when one of them is not in `tools/list` |
| minimum tools | optional: down when `tools/list` returns fewer tools |

Each check runs `initialize`, sends `notifications/initialized`, then `tools/list` (all pages) when the server
advertises tools or a tool condition is set, and finally deletes the session. Tools are never called. The result
shows the server name and version, the protocol version, the transport and the tool count, e.g.
`my-server 1.4.0 · mcp 2025-06-18 · streamable · 12 tools`. https endpoints get certificate expiry alerts.

## Heartbeat monitors (cron jobs)

For jobs that run on a schedule (backups, cron, queues), create a monitor of type **heartbeat (push)**. It gets a
secret URL; the job calls it at the end of every run, with any HTTP method:

```sh
0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 https://status.example.com/api/push/<token> >/dev/null
```

The monitor goes down when no call arrives within the **expected period** plus the **grace time**, and back up at
the next call. Optional query parameters: `status=down` reports a failed run, `msg=` stores a short note, `ping=`
the run time in ms. The URL uses the public URL from the settings and can be regenerated from the monitor page.

## Groups and tags

A monitor sits in one **group** (or none) and has any number of colored **tags**. Manage them on the **groups**
page. Then:

- filter the dashboard by group or tag, and show groups as sections;
- add whole groups to a [status page](status-pages.md): monitors added to the group later show up too;
- pick monitors by group or tag in [maintenance windows](alerts.md#maintenance-windows) and
  [notification channels](alerts.md#which-monitors-and-events-each-channel-gets);
- filter the API: `GET /api/monitors?group=<id>` (or `group=none`) and `?tag=<id>`.

## Diagnostics on failure

At the first failure of a check, the location that ran it collects diagnostics right away and sends them with the
failed result, so they are ready when the alert goes out:

- **DNS**: the A/AAAA answers (and CNAME) of the system resolver; for DNS monitors also the answer of their server
  and the NS records of the name;
- **TCP**: a connection to each resolved address (up to 4) on the monitor's port;
- **TLS**: version, certificate subject, issuer, names, expiry and the verification error;
- **HTTP**: status, redirects (without query strings) and response headers. Cookies, `Authorization` and any header
  whose name contains token, key, secret, session, csrf or auth are redacted. The body and the request headers are
  never kept;
- **traceroute**: up to 20 hops with reverse names, as TCP SYNs to the monitor's port (UDP for ping and DNS). It
  needs no root: on Linux it reads the ICMP errors like `tracepath`, so it works in the non-root containers. Other
  systems skip it for now.

It takes at most 8 seconds and 16 KB, and runs at most once every 10 minutes per monitor and location. The first
alert of an incident lists one line per failing location and links to the full diagnostics on the incidents page;
webhooks get `diagnostics` and `diagnostics_url`, reminders only the link. Diagnostics are never shown on status
pages. They are also in the API (`GET /api/incidents/{id}/diagnostics`) and the MCP tool
`get_incident_diagnostics`.

Turn them off in **settings → monitors**, or per monitor. Diagnostics of incidents are kept 30 days (configurable);
those of failures that never became an incident, one day.

## Import from Uptime Kuma

**settings → data → import from uptime kuma** reads a backup file of Uptime Kuma 1.x (Settings → Backup → Export
there) and shows what will be created before anything changes. Groups and tags come along and paused monitors stay
paused; notification channels are not imported.
