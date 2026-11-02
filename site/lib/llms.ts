import { REPO } from "@/components/site-shell"
import { allDocs, DOC_PAGES, loadDoc } from "@/lib/docs"
import { SITE } from "@/lib/seo"

// llms.txt (https://llmstxt.org): a short Markdown map of the site for AI
// assistants. llms-full.txt is every English docs page in one file, read at
// build time from docs/en, so it always matches main.

export function llmsTxt() {
  const link = (slug: (typeof DOC_PAGES)[number]) => {
    const d = loadDoc("en", slug)
    return `- [${d.title}](${SITE}/docs/${slug}/): ${d.description}`
  }
  return `# pingtxt

> Open source (AGPL-3.0), self-hosted uptime monitoring. One Docker container runs the hub (Go API, scheduler, SQLite or PostgreSQL/MariaDB, web UI); small Go agents connect to it over WebSocket and run checks from many locations. Status is decided by majority vote across locations.

Features: HTTP(S), keyword, TCP, ICMP ping, DNS, PostgreSQL, MySQL, Redis, MongoDB, Docker, gRPC, SMTP, MCP and heartbeat checks; public status pages with incidents; maintenance windows; certificate and domain expiry alerts; groups and tags; notifications; SSO and 2FA; REST API; MCP server; Prometheus metrics; iOS app.

## Docs

${DOC_PAGES.map(link).join("\n")}

## API

- [REST API reference](${SITE}/docs/api/): every endpoint with roles, parameters and examples
- [OpenAPI document](${SITE}/openapi.json): OpenAPI 3.1 JSON of the latest version
- [MCP server](${SITE}/docs/integrations/#mcp-server): the hub serves MCP on /mcp (OAuth 2.1 or API tokens)

## Optional

- [Full documentation as one file](${SITE}/llms-full.txt): every docs page
- [Source code](${REPO})
- [Releases](${REPO}/releases)
- [Privacy policy of the iOS app](${SITE}/privacy/)
- [Italian version of the site](${SITE}/it/): the same pages in Italian (docs at ${SITE}/it/docs/)
`
}

export function llmsFullTxt() {
  // Links stay relative (install.md#...): each page starts with its "# title".
  return allDocs("en")
    .map((d) => d.source.trim())
    .join("\n\n---\n\n")
}
