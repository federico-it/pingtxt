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
| http(s) | `https://example.com/health` | status code (default 200-399), headers, body, [JSON rules](#response-checks-json), TLS expiry; optional [steps before it](#multi-step-http-checks) such as a login |
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
| udp | `host:port` | a datagram gets a reply, optionally containing a text or bytes, see [UDP](#udp) |
| websocket | `wss://example.com/socket` | the connection opens; optionally a message gets the expected reply, see [WebSocket](#websocket) |
| status page (atlassian) | `https://status.example.com` | the state of an Atlassian Statuspage page or of some of its components; minor problems are degraded, see [Status pages of other services](#status-pages-of-other-services) |
| ssh | `host` or `host:port` | the SSH banner and host key, optionally pinned, see [SSH](#ssh) |
| imap, pop3 | `imaps://host`, `pop3s://host`, `imap://user:pass@host` | greeting, `STARTTLS`/`STLS` when offered, optional login, see [Mail servers](#mail-servers-imap-pop3) |
| ntp | `host` or `host:port` | the server answers, is synchronized and its clock is close to the location's, see [NTP](#ntp) |
| blacklist (rbl) | IP address or mail domain | not listed on DNS blacklists, see [Blacklists](#blacklists-rbl) |
| heartbeat (push) | none: pingtxt gives you a URL | your job calls the URL; down when the calls stop, see [heartbeat](#heartbeat-monitors-cron-jobs) |

HTTP monitors also take a method, request headers and body, the accepted status codes and whether to skip TLS
verification. Agents older than a check type fail its checks with "this agent cannot run ... checks: update it".

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

## Multi-step HTTP checks

An http(s) or keyword monitor can make other requests first, under **steps before the request** (API `steps`): log in,
take the token from the answer, then check a protected endpoint. Steps run in order, at most 10, and the monitor's own
request runs last.

Each step has a method, URL, headers, body and accepted status codes (default 200-399), and can **save values** from
its response as variables:

| from | path | example |
| --- | --- | --- |
| json path | a path in the JSON body, as in [response checks](#response-checks-json) | `data.token` |
| header | a response header | `X-Csrf-Token` |
| regex | a regular expression on the body: its first group, or the whole match | `name="csrf" value="([^"]+)"` |

Write `{{token}}` to use a variable in the URL, headers or body of the next steps and of the monitor's request, e.g.
`Authorization: Bearer {{token}}`. Cookies set by any response are sent with the next requests, so session logins
work without variables.

The check fails at the first step that fails, and the message names it: `step 1 (login): unexpected status 401` or
`step 2: {{token}}: data.token not found`. The latency covers all the requests. Step headers and bodies (passwords)
are hidden from read-only users. Agents older than steps fail the check with "update it" instead of skipping the
login.

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

While a monitor is degraded or down, its page says why at the top, and the dashboard row shows the reason in place of
the target. Locations that report the same problem are listed together, on one line: the same goes for the cause of
incidents and alerts (`agent-1, agent-2: HTTP 503`). The API has the reasons in `reasons` of every monitor.

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

## UDP

A **udp** monitor sends a **payload** to `host:port` and waits for the reply. UDP has no connection to open, so a
silent service cannot be told from a stopped one: the check passes only when a reply arrives within the timeout.
Send something the service answers, e.g. a DNS query, an NTP request or your game server's status query.

- **written as** (API `method`): `text` (default) or `hex`. In hex, bytes may be separated by spaces or colons:
  `1b 00 00 00`.
- **expected reply** (API `keyword`): text or bytes the reply must contain, in the same encoding. Empty: any reply.

The result is `reply 48 bytes`, `no reply (timeout)` or `port unreachable` when the host answers that nothing
listens on the port. Diagnostics on failure look up the name and trace the route (there is no TCP connection to try).

## WebSocket

A **websocket** monitor opens a `ws://` or `wss://` connection; a refused upgrade fails with its HTTP status, e.g.
`unexpected status 401 (no WebSocket upgrade)`.

- **message to send** (API `body`): optional text message sent once connected.
- **expected reply** (API `keyword`): text the first message from the server must contain. With a message or an
  expected reply set, the check waits for that first message.
- **headers**: sent with the handshake, e.g. `Authorization: Bearer <token>`. `Sec-WebSocket-Protocol: chat, v2`
  asks for subprotocols; the one the server picks shows in the result, e.g. `connected (chat)`.

`wss://` endpoints get certificate expiry alerts.

## SSH

An **ssh** monitor connects to `host` (port 22) or `host:port`, reads the server's version banner and runs the key
exchange up to the server's host key. It never logs in, so it needs no account. The result shows the banner and the
host key, e.g. `SSH-2.0-OpenSSH_9.6p1, ssh-ed25519 SHA256:…`.

**expected banner or host key** (API `keyword`) is optional:

- a text, e.g. `OpenSSH`: the banner must contain it;
- a host key fingerprint `SHA256:…`, as printed by `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` or shown by
  a passing check: the check fails with `host key changed` when none of the server's keys has it. Pinning the key
  tells you when a server was reinstalled, or when something else answers on its address.

## Mail servers (IMAP, POP3)

**imap** and **pop3** monitors check a mailbox server the way **smtp** checks a mail relay:

- `imaps://host` (993) and `pop3s://host` (995) use TLS from the start; `imap://host` (143) and `pop3://host` (110)
  read the greeting and upgrade with `STARTTLS` (`STLS` for POP3) when the server offers it. A bare `host[:port]`
  is the plain scheme. The certificate is verified unless **ignore TLS certificate errors** is on.
- With a user and password in the URL, e.g. `imaps://monitor%40example.com:secret@mail.example.com`, the check also
  logs in (`LOGIN`, or `USER`/`PASS`) and fails on a wrong password or a locked account. It never sends the password
  without TLS. Use a mailbox made for monitoring: admins can see the password, read-only users cannot.

The result reads e.g. `tls, logged in, * OK Dovecot ready.`.

## NTP

An **ntp** monitor asks an NTP server for the time (SNTP) on `host` (port 123) or `host:port`. It fails when the
server does not answer, answers that it is not synchronized (stratum 16 or the alarm leap indicator), refuses the
request (a "kiss-o'-death", e.g. `RATE`), or when its clock is further from the clock of the location running the
check than the **maximum offset** (API `expected_status`, in ms, default 1000). The result reads e.g.
`stratum 2, offset +1.3 ms`, and the response time is the network round trip.

The offset is measured against the location's own clock: keep the clocks of the hub and agents synchronized, or
raise the limit.

## Blacklists (RBL)

A **blacklist (rbl)** monitor looks a target up in DNS blacklists, the lists mail servers use to refuse spam. It is
down while the target is listed, and the message names the lists, e.g.
`listed: 203.0.113.7 on zen.spamhaus.org (127.0.0.2)`.

- An **IP address** (IPv4 or IPv6) is checked on the IP lists.
- A **mail domain** is checked on the domain lists, and the addresses of its MX servers (or of the domain itself
  when it has no MX record) on the IP lists. With an IP family set, only the addresses of that family are checked.
- **blacklists** (API `keyword`): the zones to query, comma-separated. Empty uses the defaults: Spamhaus ZEN,
  SpamCop, Barracuda, Mailspike, PSBL, Manitu, DroneBL and s5h for addresses, Spamhaus DBL, SURBL and URIBL for
  domains. Zones known to list domains go to the domain checks, all others to the address checks.
- **resolver** (API `dns_server`): the DNS server to ask. Blacklists refuse queries from big public resolvers such
  as 8.8.8.8 or 1.1.1.1; when no list answers the check fails with `no blacklist answered`. Run the check from a
  location with its own resolver, or set your provider's. Answers that mean "query refused" (127.0.0.1,
  127.255.255.x) never count as a listing.

Check every hour or less often: lists change slowly and some limit the number of free queries.

## Client certificates (mutual TLS)

**http**, **keyword**, **websocket**, **grpc** and **mcp** monitors can present a client certificate to servers
that ask for one (mutual TLS). Click **add client certificate (mTLS)** and paste the certificate (PEM, the chain may
follow it) and its private key (PEM, not encrypted); the hub checks that they match before saving (API
`client_cert` and `client_key`). An expired certificate fails the check with its date. The key is hidden from
read-only users; other types ignore the certificate. Agents older than this setting fail the check with "this agent
cannot send client certificates: update it".

## Status pages of other services

Many services publish their state on an **Atlassian Statuspage** page, e.g. `status.claude.com`,
`www.githubstatus.com` or `www.cloudflarestatus.com`. A **status page (atlassian)** monitor reads its public API
(`/api/v2/summary.json`) and follows what the provider reports:

| page or component state | monitor |
| --- | --- |
| all systems operational | up |
| minor problem: degraded performance or partial outage | degraded |
| major or critical problem: major outage | down |
| maintenance in progress | up, with the maintenance in the message |

The check message names the open incidents, e.g. `Minor Service Outage · Incident: Elevated errors on Claude Opus
(identified)`, and so do the alerts. Degraded sends the **degraded** channel event, not a down alert.

- **target**: the address of the page. Any URL of the site works: `https://status.claude.com/api/v2/status.json` is
  saved as `https://status.claude.com`.
- **components to follow** (API `keyword`, comma-separated component ids): the form lists the components of the page
  to tick. With none chosen the overall state of the page counts; with some, only their state and their incidents
  do, so a problem with claude.ai does not touch a monitor that follows the Claude API.
- **locations**: the page gives the same answer from everywhere, so a new monitor of this type checks it from the hub
  only, and adding a new agent does not offer it. More locations only repeat the same request.

When you type the URL of such a page in an **http(s)** or **keyword** monitor, the form offers to switch to this type
with one click. Before it, the usual way was a JSON rule such as `status.indicator` equals `none`, which turns the
monitor down at the first minor incident.

There are no diagnostics and no certificate or domain expiry alerts for this type: the page belongs to the provider.

### Popular services

**monitors → services** lists about 70 popular services that publish such a page, by category: AI (Claude, OpenAI,
Mistral...), developer tools (GitHub, npm, Docker Hub...), cloud and hosting (Cloudflare, Vercel, DigitalOcean...),
observability, email and messaging, productivity, commerce and social. Search by name or product (`chatgpt`,
`copilot`), tick the ones you depend on and press **add**: each becomes a status page monitor named after the service,
checked from the hub every 2 minutes (1 or 5 if you pick so), in the group you choose (by default a new group called
**services**). Services that a status page monitor already follows show **following** and link to it. Logos come
from [Dashboard Icons](https://dashboardicons.com) and ship with the hub, so the page loads nothing from outside.

The whole page is followed. To follow only some components, edit the monitor afterwards. A service missing from the
list works the same way with a custom monitor of this type, if it has an Atlassian Statuspage page.

API: `GET /api/monitors/presets` (the catalog, with `monitor_id` of the monitor that already follows each service) and
`POST /api/monitors/presets` with `{"ids": ["claude", "github"], "group_name": "services"}`. MCP:
`list_service_presets` and `add_service_presets`.

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
- filter the API: `GET /api/monitors?group=<id>` (or `group=none`) and `?tag=<id>`; add `include_subgroups=true` to
  get the subgroups' monitors too.

A group can sit inside another one (**inside group** in the group dialog), up to 3 levels, for example *project* with
*api*, *site* and *database*. Subgroups show nested under their group on the dashboard and the groups page, and as
*project › api* on status pages, in pickers and in reports. A group's header sums up its subgroups, and picking a
group for a status page, maintenance window, channel or report includes its subgroups. Deleting a group moves its
subgroups up one level.

On the dashboard a line on the left links each subgroup and its monitors to the group above, and **fold all** /
**unfold all** next to the group filter closes or opens every section. The **group** filter is a menu with the
groups as a tree and how many monitors each one holds; picking a group shows its subgroups too. On the groups page
**+ subgroup** creates a group inside that one, the arrow folds its subgroups, and the monitor count opens the
dashboard filtered on that group.

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
  systems skip it for now;
- **SMTP greeting**: for SMTP monitors, the first line the server sends (e.g. `421 too many connections`).

Docker and push monitors get no diagnostics: there is no network path to look at.

### Reading the diagnosis

The hub reads the data and puts the result in plain words at the top: the **diagnosis**, the other problems it found
and the error of the check. For example:

- the name does not exist (NXDOMAIN), or the DNS server of a DNS monitor answers differently from the system resolver;
- the port refuses the connection (nothing listens there) or does not answer (a firewall or a host that is off), with
  the hop where the route stops; a port that works over IPv4 but not over IPv6;
- the certificate expired, is not valid for the name, or is not trusted (self-signed or missing intermediate);
- the HTTP status explained: 502/504 from a proxy with nothing behind it, the 52x errors of Cloudflare (the origin is
  the problem), 503 and 429 with their `Retry-After`, 401/403 access denied;
- a host that is reachable but does not answer the ping (ICMP blocked), or a mail server that greets with an error;
- when DNS, connection and TLS all work, that the problem is in the service itself.

Below it, every step (DNS, connection, TLS, HTTP response, route) has one line with ✓ or ✗; click a step for its raw
data (addresses, certificate names, response headers, the hops of the traceroute). **Copy as text** copies all of it,
diagnosis included, for a ticket.

It takes at most 8 seconds and 16 KB, and runs at most once every 10 minutes per monitor and location. The first
alert of an incident lists one line per failing location, starting with the diagnosis, and links to the full diagnostics on the incidents page;
webhooks get `diagnostics` and `diagnostics_url`, reminders only the link. Diagnostics are never shown on status
pages. They are also in the API (`GET /api/incidents/{id}/diagnostics`) and the MCP tool
`get_incident_diagnostics`.

Turn them off in **settings → monitors**, or per monitor. Diagnostics of incidents are kept 30 days (configurable);
those of failures that never became an incident, one day.

## Import from Uptime Kuma

**settings → data → import from uptime kuma** reads a backup file of Uptime Kuma 1.x (Settings → Backup → Export
there) and shows what will be created before anything changes. Groups and tags come along and paused monitors stay
paused; notification channels are not imported.
