# Integrations

Use pingtxt from other tools: scripts and apps through the REST API, AI assistants through the MCP server,
Prometheus through `/metrics`.

## API tokens

Create personal tokens in **settings → tokens** and send them as a bearer token:

```sh
curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/monitors
```

- **read only** tokens can call `GET` endpoints. **read + write** tokens can also create, edit, pause and delete.
- A token never has more rights than its owner: read-only users only get read tokens, and the write tokens of an
  admin who becomes read-only stop writing.
- Tokens can expire after 30, 90 or 365 days, or never. They are shown once and stored hashed.
- Tokens never reach account endpoints (password, 2FA, tokens, users, instance settings): those need a browser
  login.

## REST API

Every endpoint, with parameters, roles and examples, is in the [API reference](https://pingtxt.com/docs/api/). Each
hub also serves:

- the OpenAPI 3.1 document at `/api/openapi.json`, to generate a client;
- an interactive Swagger UI at `/api/docs` (linked from **settings → tokens**), where requests run with your browser
  session or a token set in *Authorize*.

`PUT` on a monitor, maintenance window, group, tag or status page, and on the general, backup and report settings,
changes only the fields in the body: the others keep their value. Send a field to change it, also to empty it (`[]`,
`""`, `0`). A script or an older app that does not know a newer field so never resets it.

## MCP server

The hub is also an [MCP](https://modelcontextprotocol.io) server at `/mcp` (Streamable HTTP), so an AI assistant
such as Claude can answer "what is down right now?", look at incidents, response times and diagnostics, or create a
monitor or a maintenance window for you. (To *monitor* an MCP server instead, see
[MCP servers](monitors.md#mcp-servers).)

### Connect with OAuth

Nothing to copy: add `https://status.example.com/mcp` as a connector.

- claude.ai and Claude Desktop: **settings → connectors → add custom connector**.
- Claude Code: `claude mcp add --transport http pingtxt https://status.example.com/mcp`, then `/mcp` to log in.

The assistant opens pingtxt in the browser, you log in as usual and pick **read only** or **read + write** (admins
only). The hub supports discovery (RFC 9728 / RFC 8414), dynamic client registration, PKCE (S256) and rotating
refresh tokens. Access tokens last an hour and work on `/mcp` and the REST API. Connected apps are listed in
**settings → tokens**, where you can disconnect them. Behind a proxy, set the public URL in **settings → general**
so OAuth redirects point to the right address.

### Connect with an API token

For clients without OAuth, send `Authorization: Bearer ptk_...`. A read only token (or the token of a read-only
user) gets the read tools only; a read + write token of an admin also gets the tools that change data.

Claude Code:

```sh
claude mcp add --transport http pingtxt https://status.example.com/mcp \
  --header "Authorization: Bearer ptk_..."
```

Claude Desktop (`claude_desktop_config.json`, through [mcp-remote](https://www.npmjs.com/package/mcp-remote)):

```json
{
  "mcpServers": {
    "pingtxt": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://status.example.com/mcp", "--header", "Authorization:${PINGTXT_AUTH}"],
      "env": { "PINGTXT_AUTH": "Bearer ptk_..." }
    }
  }
}
```

Cursor, Windsurf and other clients that take a URL and headers:

```json
{ "mcpServers": { "pingtxt": { "url": "https://status.example.com/mcp", "headers": { "Authorization": "Bearer ptk_..." } } } }
```

### Tools

| tool | does |
| --- | --- |
| `get_overview` | monitors by status, agents online, open incidents |
| `list_monitors` | status, 24h uptime, response time and last check; filter by status, group, tag or name |
| `get_monitor`, `get_monitor_results` | settings, uptime 24h/7d/30d, status per location, expiry; latest check results |
| `list_incidents`, `get_incident_diagnostics` | incidents (open only, per monitor); what each location saw at the first failure |
| `list_maintenances` | maintenance windows with their next occurrence |
| `list_groups`, `list_tags`, `list_agents`, `list_status_pages` | everything else a model needs to find ids |
| `create_monitor`, `update_monitor`, `pause_monitor`, `delete_monitor` | write token of an admin only |
| `acknowledge_incident` | write token of an admin only; the incident is acknowledged by the token's user |
| `create_maintenance`, `delete_maintenance` | write token of an admin only; times in ISO 8601 |

Roles, validation and hidden secrets are exactly those of the REST API, and every write is in the
[audit log](security.md#audit-log). Times in results are ISO 8601 (UTC). The server is stateless (no sessions, no
event stream), so it works behind any reverse proxy and with several hub replicas.

## Prometheus

`GET /metrics` serves metrics in the Prometheus text format. It needs an API token (read is enough), unless
**settings → integrations → prometheus metrics → public /metrics** is on. The output is cached for 10 seconds.

```yaml
scrape_configs:
  - job_name: pingtxt
    scheme: https
    authorization:
      credentials: ptk_...
    static_configs:
      - targets: ["status.example.com"]
```

| metric | labels | meaning |
| --- | --- | --- |
| `pingtxt_monitor_up` | monitor_id, monitor, type | 1 up or degraded, 0 down (absent while pending, paused, in maintenance) |
| `pingtxt_monitor_status` | + status | 1 for the current status (up, down, degraded, pending, paused, maintenance) |
| `pingtxt_monitor_incident_open` | monitor | 1 while an incident is open |
| `pingtxt_monitor_uptime_ratio` | + window (24h, 7d, 30d) | share of checks taken while the monitor was up or degraded, maintenance excluded |
| `pingtxt_monitor_latency_avg_seconds` | + window | average response time of successful checks |
| `pingtxt_check_up`, `pingtxt_check_latency_seconds`, `pingtxt_check_status_code`, `pingtxt_check_timestamp_seconds` | + location, location_name | last check from each location |
| `pingtxt_check_phase_seconds` | + location, location_name, phase (dns, connect, tls, ttfb) | time per phase of the last HTTP check from each location |
| `pingtxt_cert_expiry_days`, `pingtxt_domain_expiry_days` (and `_timestamp_seconds`) | + subject | days left before expiry |
| `pingtxt_agent_connected`, `pingtxt_agent_last_seen_timestamp_seconds` | agent_id, agent, location, location_name, version | agent connectivity |
| `pingtxt_monitors`, `pingtxt_agents`, `pingtxt_incidents_open`, `pingtxt_info` | | totals and hub version |
