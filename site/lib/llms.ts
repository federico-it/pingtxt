import { REPO } from "@/components/site-shell"
import { allDocs, DOC_GROUPS, loadDoc, neighbours, type DocSlug } from "@/lib/docs"
import { localePath, tx, type Lang } from "@/lib/i18n"
import { loadOpenAPI } from "@/lib/openapi"
import { SITE } from "@/lib/seo"

// Files for AI assistants and agents, all built from docs/ at build time so
// they always match main:
// - /llms.txt (https://llmstxt.org): a Markdown map of the docs, page by
//   page and section by section, with how to read them;
// - /llms-full.txt: every docs page in one file;
// - /docs/<page>.md (and /docs/<page>/index.html.md, the llms.txt
//   convention): one page as Markdown, links pointing to other .md pages;
// - /docs/api.md: the REST API endpoints, from the OpenAPI document.
// The Italian ones are the same under /it/.

// Where the Markdown of a page is: pingtxt.com/docs/install.md, the index is
// pingtxt.com/docs.md. scripts/md-alias.mjs copies index.html.md there.
export function mdPath(lang: Lang, slug: DocSlug | "" | "api") {
  return localePath(lang, slug ? `/docs/${slug}.md` : "/docs.md")
}

export function htmlPath(lang: Lang, slug: DocSlug | "" | "api") {
  return localePath(lang, slug ? `/docs/${slug}/` : "/docs/")
}

const abs = (p: string) => `${SITE}${p}`

// The anchor in `lang` of a section known by its English anchor (the
// languages have the same headings in the same order, see checkDocs).
function anchor(lang: Lang, slug: DocSlug, enId: string) {
  const i = loadDoc("en", slug).headings.findIndex((h) => h.id === enId)
  if (i < 0) throw new Error(`docs/en/${slug}.md has no #${enId}`)
  return `${abs(mdPath(lang, slug))}#${loadDoc(lang, slug).headings[i].id}`
}

// A link of a docs file, as an absolute URL for the Markdown versions: other
// pages go to their .md, repository files to GitHub.
function mdHref(lang: Lang, href: string) {
  if (/^(https?:|mailto:)/.test(href) || href.startsWith("#")) return href
  const [file, hash] = href.split("#")
  const tail = hash ? `#${hash}` : ""
  if (file.startsWith("../../")) return `${REPO}/blob/main/${file.slice(6)}${tail}`
  const slug = file === "README.md" ? "" : (file.replace(/\.md$/, "") as DocSlug)
  return abs(mdPath(lang, slug)) + tail
}

// The Markdown of a page with its links rewritten; code blocks stay as they are.
function rewrite(lang: Lang, source: string) {
  return source
    .trim()
    .split(/(^```[\s\S]*?^```)/m)
    .map((part, i) =>
      i % 2 ? part : part.replace(/(!?\[[^\]]*\]\()([^)\s]+)(\))/g, (_, a, href, b) => a + mdHref(lang, href) + b),
    )
    .join("")
}

// One docs page as Markdown, with a footer to move around without HTML.
export function docMarkdown(lang: Lang, slug: DocSlug | "") {
  const t = tx(lang)
  const d = loadDoc(lang, slug)
  const { prev, next } = neighbours(slug)
  const nav = [
    prev !== undefined && `${t("Previous", "Precedente")}: [${loadDoc(lang, prev).title}](${abs(mdPath(lang, prev))})`,
    next !== undefined && `${t("Next", "Successiva")}: [${loadDoc(lang, next).title}](${abs(mdPath(lang, next))})`,
    `${t("All pages", "Tutte le pagine")}: ${abs(localePath(lang, "/llms.txt"))}`,
  ].filter(Boolean)
  return `${rewrite(lang, d.source)}

---

${t("Markdown version of", "Versione Markdown di")} ${abs(htmlPath(lang, slug))}
${nav.join("\n")}
`
}

const ACCESS: Record<string, [string, string]> = {
  public: ["no authentication", "senza autenticazione"],
  read: ["read token or any user", "token di lettura o qualsiasi utente"],
  write: ["write token of an admin", "token di scrittura di un admin"],
  session: ["browser session only", "solo sessione del browser"],
  admin: ["admin session only", "solo sessione di un admin"],
}

// The REST API as a list of endpoints per tag. Descriptions stay in English,
// like on the HTML page; the OpenAPI document has every detail.
export function apiMarkdown(lang: Lang) {
  const t = tx(lang)
  const doc = loadOpenAPI()
  const ops = Object.entries(doc.paths).flatMap(([p, methods]) =>
    Object.entries(methods).map(([m, op]) => ({ path: p, method: m.toUpperCase(), op })),
  )
  const out = [
    t("# REST API reference", "# Riferimento della REST API"),
    "",
    t(
      `Every endpoint of the hub, under \`/api\` of your own hub. Send an API token as \`Authorization: Bearer <token>\` (see [API tokens](${anchor(lang, "integrations", "api-tokens")})). Request and response schemas are in the OpenAPI 3.1 document: ${abs("/openapi.json")}.`,
      `Tutti gli endpoint dell'hub, sotto \`/api\` del tuo hub. Manda un token API come \`Authorization: Bearer <token>\` (vedi [Token API](${anchor(lang, "integrations", "api-tokens")})). Gli schemi di richieste e risposte sono nel documento OpenAPI 3.1: ${abs("/openapi.json")}. Le descrizioni degli endpoint sono in inglese.`,
    ),
    "",
    t("Access levels:", "Livelli di accesso:"),
    "",
    ...Object.entries(ACCESS).map(([k, v]) => `- \`${k}\`: ${t(...v)}`),
  ]
  for (const tag of doc.tags) {
    const list = ops.filter((o) => o.op.tags?.[0] === tag.name)
    if (!list.length) continue
    out.push("", `## ${tag.name}`, "")
    if (tag.description) out.push(tag.description, "")
    for (const o of list) {
      out.push(`- \`${o.method} ${o.path}\` (${o.op["x-pingtxt-access"]}): ${o.op.summary}`)
    }
  }
  out.push(
    "",
    "---",
    "",
    `${t("Markdown version of", "Versione Markdown di")} ${abs(htmlPath(lang, "api"))}`,
    `${t("All pages", "Tutte le pagine")}: ${abs(localePath(lang, "/llms.txt"))}`,
    "",
  )
  return out.join("\n")
}

export function llmsTxt(lang: Lang) {
  const t = tx(lang)
  const page = (slug: DocSlug | "") => {
    const d = loadDoc(lang, slug)
    const md = abs(mdPath(lang, slug))
    const sections = d.headings
      .filter((h) => h.depth === 2)
      .map((h) => `  - [${h.text}](${md}#${h.id})`)
    return [`- [${d.title}](${md}): ${d.description}`, ...sections].join("\n")
  }
  const groups = DOC_GROUPS.map((g) => {
    const head = g[lang] ? `### ${g[lang]}\n\n` : ""
    return head + g.pages.map(page).join("\n")
  })
  const other = lang === "en" ? "it" : "en"
  return `# pingtxt

> ${t(
    "Open source (AGPL-3.0), self-hosted uptime monitoring. One Docker container runs the hub (Go API, scheduler, SQLite or PostgreSQL/MariaDB, web UI); small Go agents connect to it over WebSocket and run checks from many locations. The status of a monitor is decided by a vote across its locations.",
    "Monitoraggio dell'uptime open source (AGPL-3.0) e self-hosted. Un container Docker esegue l'hub (API in Go, scheduler, SQLite o PostgreSQL/MariaDB, interfaccia web); piccoli agent in Go si collegano via WebSocket ed eseguono i controlli da più località. Lo stato di un monitor è deciso da un voto tra le sue località.",
  )}

${t(
  "Checks: HTTP(S) with JSON rules and multi-step requests, keyword, WebSocket, gRPC, MCP servers, ping, TCP, UDP, DNS, NTP, SSH, SMTP, IMAP/POP3, DNS blacklists, PostgreSQL, MySQL/MariaDB, Redis, MongoDB, server CPU/RAM/disk, Docker containers, status pages of other services (Atlassian Statuspage, Apple) and heartbeats for cron jobs. Also: public status pages with incidents, subscribers, feeds and webhooks; notification channels, escalation and on-call schedules; maintenance windows; certificate and domain expiry alerts; groups, tags and dependencies; roles, SSO and 2FA; REST API, MCP server, Prometheus metrics, configuration as code; iOS app.",
  "Controlli: HTTP(S) con regole JSON e richieste in più passi, keyword, WebSocket, gRPC, server MCP, ping, TCP, UDP, DNS, NTP, SSH, SMTP, IMAP/POP3, blacklist DNS, PostgreSQL, MySQL/MariaDB, Redis, MongoDB, CPU/RAM/disco dei server, container Docker, status page di altri servizi (Atlassian Statuspage, Apple) e heartbeat per i cron job. Inoltre: status page pubbliche con incidenti, iscritti, feed e webhook; canali di notifica, escalation e turni di reperibilità; finestre di manutenzione; avvisi di scadenza di certificati e domini; gruppi, tag e dipendenze; ruoli, SSO e 2FA; REST API, server MCP, metriche Prometheus, configurazione come codice; app iOS.",
)}

## ${t("How to read these docs", "Come leggere questa documentazione")}

${t(
  `- Every page below is plain Markdown: the links go to the .md version. The same page as HTML is at the URL without ".md" plus a slash (${SITE}/docs/install.md is ${SITE}/docs/install/).
- The sections of each page are listed under it; their links jump to the heading (GitHub anchor rules).
- Links inside a page point to other .md pages, and every page ends with links to the previous and next page and back to this file.
- To read everything at once, fetch ${abs(localePath(lang, "/llms-full.txt"))} (all pages in one file, in this order).
- Settings mentioned as "API \`field\`" are fields of the REST API; the [REST API reference](${abs(mdPath(lang, "api"))}) lists every endpoint.
- Start from [Concepts](${abs(mdPath(lang, "concepts"))}) for the vocabulary (hub, location, agent, monitor, incident) and [Install](${abs(mdPath(lang, "install"))}) to run it.`,
  `- Ogni pagina qui sotto è Markdown semplice: i link portano alla versione .md. La stessa pagina in HTML è all'URL senza ".md" più una barra (${SITE}/it/docs/install.md è ${SITE}/it/docs/install/).
- Le sezioni di ogni pagina sono elencate sotto di essa; i loro link saltano al titolo (regole delle ancore di GitHub).
- I link dentro una pagina portano ad altre pagine .md, e ogni pagina finisce con i link alla pagina precedente, alla successiva e a questo file.
- Per leggere tutto in una volta, scarica ${abs(localePath(lang, "/llms-full.txt"))} (tutte le pagine in un file, in questo ordine).
- Le impostazioni citate come "API \`campo\`" sono campi della REST API; il [riferimento della REST API](${abs(mdPath(lang, "api"))}) elenca tutti gli endpoint.
- Parti da [Concetti](${abs(mdPath(lang, "concepts"))}) per il vocabolario (hub, località, agent, monitor, incidente) e da [Installazione](${abs(mdPath(lang, "install"))}) per avviarlo.`,
)}

## ${t("Docs", "Documentazione")}

${page("")}

${groups.join("\n\n")}

## API

- [${t("REST API reference", "Riferimento della REST API")}](${abs(mdPath(lang, "api"))}): ${t("every endpoint with its access level", "ogni endpoint con il suo livello di accesso")}
- [${t("OpenAPI document", "Documento OpenAPI")}](${abs("/openapi.json")}): ${t("OpenAPI 3.1 JSON of the latest version, with every schema", "JSON OpenAPI 3.1 dell'ultima versione, con tutti gli schemi")}
- [${t("MCP server", "Server MCP")}](${anchor(lang, "integrations", "mcp-server")}): ${t("the hub serves MCP on /mcp (OAuth 2.1 or API tokens)", "l'hub espone MCP su /mcp (OAuth 2.1 o token API)")}

## Optional

- [${t("Full documentation as one file", "Tutta la documentazione in un file")}](${abs(localePath(lang, "/llms-full.txt"))})
- [${t("Source code", "Codice sorgente")}](${REPO})
- [Releases](${REPO}/releases)
- [${t("Privacy policy of the iOS app", "Privacy dell'app iOS")}](${abs(localePath(lang, "/privacy/"))})
- [${t("Italian version of these docs", "Versione inglese di questa documentazione")}](${abs(localePath(other, "/llms.txt"))})
`
}

export function llmsFullTxt(lang: Lang) {
  const t = tx(lang)
  const head = t(
    `<!-- pingtxt documentation, every page of ${SITE}/docs/ in one file. Map of the pages: ${SITE}/llms.txt. Links to other pages point to their .md version; their content is also below, after the page with that title. -->`,
    `<!-- Documentazione di pingtxt, tutte le pagine di ${SITE}/it/docs/ in un file. Mappa delle pagine: ${SITE}/it/llms.txt. I link alle altre pagine portano alla loro versione .md; il loro contenuto è anche qui sotto, nella pagina con quel titolo. -->`,
  )
  const pages = allDocs(lang).map(
    (d) => `<!-- ${abs(htmlPath(lang, d.slug))} -->\n\n${rewrite(lang, d.source)}`,
  )
  return [head, ...pages].join("\n\n---\n\n") + "\n"
}

export const TEXT = { "Content-Type": "text/plain; charset=utf-8" }
export const MARKDOWN = { "Content-Type": "text/markdown; charset=utf-8" }
