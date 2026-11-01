import { readFileSync } from "node:fs"
import path from "node:path"

import { REPO } from "@/components/site-shell"
import { SITE } from "@/lib/seo"

// llms.txt (https://llmstxt.org): a short Markdown map of the site for AI
// assistants. llms-full.txt is the whole README, read at build time like the
// OpenAPI document, so it always matches main.

const docs: [string, string, string][] = [
  ["hub", "Run the hub", "Docker Compose, env vars, data volume"],
  ["database", "PostgreSQL or MariaDB", "optional external database instead of SQLite"],
  ["reverse-proxy", "Reverse proxy", "TLS, WebSocket for agents"],
  ["agents", "Add a location (agent)", "one-line install, Docker or systemd"],
  ["agent-updates", "Agent updates", "self-update on reconnect, checksum, rollback"],
  ["checks", "Check types", "HTTP, keyword, TCP, ping, DNS, databases, Docker, gRPC, SMTP, heartbeat"],
  ["phases", "Response time per phase", "DNS, connect, TLS, first byte"],
  ["ipv6", "IPv6", "per monitor and per location"],
  ["response-checks", "Response checks (JSON)", "rules on JSON response bodies"],
  ["mcp-checks", "MCP server checks", "monitor an MCP server and its tools"],
  ["heartbeat", "Heartbeat monitors", "cron jobs that ping the hub"],
  ["status-pages", "Status pages", "public pages, incidents, subscribers, custom domains, password"],
  ["maintenance", "Maintenance windows", "one-off or recurring, no alerts"],
  ["expiry", "Expiry alerts", "TLS certificates and domain registration"],
  ["notifications", "Notifications", "webhook, Discord, Slack, Telegram, ntfy, email, iOS app, escalation"],
  ["sso", "Single sign-on", "GitHub, Google, OpenID Connect"],
  ["2fa", "Two-factor authentication", "TOTP"],
  ["api", "API tokens", "read or read+write personal tokens"],
  ["mcp", "MCP server", "let Claude and other AI assistants check status and manage monitors"],
  ["apps", "Native apps", "iOS app, push notifications"],
  ["prometheus", "Prometheus", "/metrics endpoint"],
  ["upgrade", "Upgrade and backup", "backups, config export/import, restore"],
  ["verify", "Verify a release", "signed checksums and images"],
  ["build", "Build from source", ""],
]

export function llmsTxt() {
  const link = ([id, title, note]: [string, string, string]) =>
    `- [${title}](${SITE}/docs/#${id})${note ? `: ${note}` : ""}`
  return `# pingtxt

> Open source (AGPL-3.0), self-hosted uptime monitoring. One Docker container runs the hub (Go API, scheduler, SQLite or PostgreSQL/MariaDB, web UI); small Go agents connect to it over WebSocket and run checks from many locations. Status is decided by majority vote across locations.

Features: HTTP(S), keyword, TCP, ICMP ping, DNS, PostgreSQL, MySQL, Redis, MongoDB, Docker, gRPC, SMTP, MCP and heartbeat checks; public status pages with incidents; maintenance windows; certificate and domain expiry alerts; groups and tags; notifications; SSO and 2FA; REST API; MCP server; Prometheus metrics; iOS app.

## Docs

${docs.map(link).join("\n")}

## API

- [REST API reference](${SITE}/docs/api/): every endpoint with roles, parameters and examples
- [OpenAPI document](${SITE}/openapi.json): OpenAPI 3.1 JSON of the latest version
- [MCP server](${SITE}/docs/#mcp): the hub serves MCP on /mcp (OAuth 2.1 or API tokens)

## Optional

- [Full documentation as one file](${SITE}/llms-full.txt): the README
- [Source code](${REPO})
- [Releases](${REPO}/releases)
- [Privacy policy of the iOS app](${SITE}/privacy/)
`
}

export function llmsFullTxt() {
  return readFileSync(path.join(process.cwd(), "..", "README.md"), "utf8")
}
