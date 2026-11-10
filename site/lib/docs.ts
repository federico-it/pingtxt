import { readFileSync } from "node:fs"
import path from "node:path"

import { Marked, type Token, type Tokens } from "marked"

import { LANGS, localePath, type Lang } from "@/lib/i18n"

// The docs are Markdown files in the repository's docs/ folder, one per page
// and language (docs/en/install.md, docs/it/install.md), read at build time
// like the OpenAPI document. README.md of each language is the /docs/ index.
// GitHub shows the same files, so links between pages are relative .md links
// and anchors follow GitHub's rules (see slugify).

export const REPO = "https://github.com/federico-it/pingtxt"
const ROOT = path.join(process.cwd(), "..", "docs")

// Order of the pages in the sidebar and in llms.txt. A new page needs its file
// in every language and an entry here.
export const DOC_PAGES = [
  "concepts",
  "install",
  "locations",
  "monitors",
  "alerts",
  "status-pages",
  "ios-app",
  "security",
  "integrations",
  "administration",
] as const
export type DocSlug = (typeof DOC_PAGES)[number]

// The sidebar groups, in DOC_PAGES order (checkDocs verifies it). The index
// page and the first group have no heading; the REST API reference closes
// the last group.
export const DOC_GROUPS: { en: string; it: string; pages: DocSlug[] }[] = [
  { en: "", it: "", pages: ["concepts"] },
  { en: "Setup", it: "Installazione", pages: ["install", "locations"] },
  { en: "Monitoring", it: "Monitoraggio", pages: ["monitors", "alerts", "status-pages", "ios-app"] },
  { en: "Operation", it: "Gestione", pages: ["security", "integrations", "administration"] },
]

export type Heading = { depth: number; text: string; id: string }
export type Doc = {
  lang: Lang
  slug: DocSlug | ""
  file: string
  title: string
  description: string
  tokens: Token[]
  headings: Heading[]
  source: string
}

// GitHub's anchor for a heading: lower case, punctuation dropped (letters of
// any script, digits, spaces, "-" and "_" kept), spaces to "-". Repeated
// headings get -1, -2...
export function slugify(text: string, seen: Map<string, number>) {
  const base = text
    .toLowerCase()
    .replace(/<[^>]+>/g, "")
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .trim()
    .replace(/\s/g, "-")
  const n = seen.get(base) ?? 0
  seen.set(base, n + 1)
  return n ? `${base}-${n}` : base
}

// Plain text of inline Markdown, for titles, descriptions and anchors.
export function plain(md: string) {
  return md
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[`*_]/g, "")
    .replace(/<[^>]+>/g, "")
    .replace(/\s+/g, " ")
    .trim()
}

const cache = new Map<string, Doc>()

export function docFile(slug: DocSlug | "") {
  return slug ? `${slug}.md` : "README.md"
}

export function loadDoc(lang: Lang, slug: DocSlug | ""): Doc {
  const key = `${lang}/${slug}`
  const hit = cache.get(key)
  if (hit) return hit
  const file = docFile(slug)
  const source = readFileSync(path.join(ROOT, lang, file), "utf8")
  const tokens = new Marked().lexer(source)
  const seen = new Map<string, number>()
  const headings: Heading[] = []
  let title = ""
  let description = ""
  for (const t of tokens) {
    if (t.type === "heading") {
      const text = plain((t as Tokens.Heading).text)
      if (t.depth === 1 && !title) title = text
      headings.push({ depth: t.depth, text, id: slugify(text, seen) })
    } else if (t.type === "paragraph" && !description) {
      description = plain((t as Tokens.Paragraph).text)
    }
  }
  if (!title) throw new Error(`docs/${lang}/${file}: no "# title"`)
  const doc: Doc = { lang, slug, file, title, description, tokens, headings, source }
  cache.set(key, doc)
  return doc
}

export function allDocs(lang: Lang) {
  return (["", ...DOC_PAGES] as const).map((s) => loadDoc(lang, s))
}

// The anchor in `lang` of a section known by its English anchor: the docs of
// every language have the same headings in the same order (checked below).
export function docHref(lang: Lang, slug: DocSlug | "", enId?: string) {
  const page = localePath(lang, slug ? `/docs/${slug}/` : "/docs/")
  if (!enId) return page
  const en = loadDoc("en", slug).headings
  const i = en.findIndex((h) => h.id === enId)
  if (i < 0) throw new Error(`docs/en/${docFile(slug)} has no #${enId}`)
  return `${page}#${loadDoc(lang, slug).headings[i].id}`
}

// Where a link in a doc points on the site. `null` for links that GitHub
// resolves but the site cannot (none today).
export function siteHref(lang: Lang, href: string) {
  if (/^(https?:|mailto:)/.test(href)) {
    const m = href.match(/^https:\/\/pingtxt\.com(\/.*)$/)
    return m ? localePath(lang, m[1]) : href
  }
  if (href.startsWith("#")) return href
  const [file, hash] = href.split("#")
  if (file.startsWith("../../")) return `${REPO}/blob/main/${file.slice(6)}${hash ? `#${hash}` : ""}`
  const slug = file === "README.md" ? "" : file.replace(/\.md$/, "")
  return localePath(lang, slug ? `/docs/${slug}/` : "/docs/") + (hash ? `#${hash}` : "")
}

// Build-time checks, so a broken docs change fails the site build:
// - every page exists in every language with the same headings (depth and
//   order), which keeps translations complete and anchors mappable;
// - every relative link points to an existing page and anchor.
let checked = false
export function checkDocs() {
  if (checked) return
  const errors: string[] = []
  const en = allDocs("en")
  for (const lang of LANGS) {
    const docs = allDocs(lang)
    docs.forEach((d, i) => {
      const ref = en[i].headings
      const shape = (h: Heading[]) => h.map((x) => x.depth).join("")
      if (lang !== "en" && shape(d.headings) !== shape(ref)) {
        errors.push(
          `docs/${lang}/${d.file}: headings differ from docs/en/${d.file} (${d.headings.length} vs ${ref.length}); translate every section`,
        )
      }
      for (const href of links(d.tokens)) {
        if (/^(https?:|mailto:)/.test(href) || href.startsWith("../../")) continue
        const [file, hash] = href.split("#")
        const target = file ? docs.find((x) => x.file === file) : d
        if (!target) {
          errors.push(`docs/${lang}/${d.file}: link to missing page ${href}`)
        } else if (hash && !target.headings.some((h) => h.id === hash)) {
          errors.push(`docs/${lang}/${d.file}: link to missing anchor ${href}`)
        }
      }
    })
  }
  if (DOC_GROUPS.flatMap((g) => g.pages).join() !== DOC_PAGES.join()) {
    errors.push("site/lib/docs.ts: DOC_GROUPS must list every page of DOC_PAGES, in the same order")
  }
  if (errors.length) throw new Error(`docs check failed:\n${errors.join("\n")}`)
  checked = true
}

function links(tokens: Token[]): string[] {
  const out: string[] = []
  new Marked().walkTokens(tokens, (t) => {
    if (t.type === "link") out.push((t as Tokens.Link).href)
  })
  return out
}

// Markdown to HTML for one block, with links rewritten for the site.
export function blockHtml(lang: Lang, token: Token) {
  return renderer(lang).parser([token])
}

// Inline Markdown (a heading's text) to HTML.
export function inlineHtml(lang: Lang, md: string) {
  return renderer(lang).parseInline(md) as string
}

function renderer(lang: Lang) {
  return new Marked({
    renderer: {
      link({ href, tokens }: Tokens.Link) {
        const to = siteHref(lang, href)
        const ext = /^https?:/.test(to) && !to.startsWith("https://pingtxt.com")
        return `<a href="${to}"${ext ? ' rel="noopener"' : ""}>${this.parser.parseInline(tokens)}</a>`
      },
    },
  })
}

// The previous and next page in the sidebar order.
export function neighbours(slug: DocSlug | "") {
  const all = ["", ...DOC_PAGES] as const
  const i = all.indexOf(slug)
  return { prev: i > 0 ? all[i - 1] : undefined, next: i < all.length - 1 ? all[i + 1] : undefined }
}

// Old single-page anchors (pingtxt.com/docs/#upgrade) and where they live now,
// as [page, English anchor]. The /docs/ page redirects them in the browser.
export const OLD_ANCHORS: Record<string, [DocSlug, string?]> = {
  hub: ["install", "run-the-hub"],
  database: ["install", "postgresql-or-mariadb"],
  "reverse-proxy": ["install", "reverse-proxy"],
  agents: ["locations"],
  "agent-updates": ["locations", "agent-updates"],
  checks: ["monitors", "check-types"],
  phases: ["monitors", "response-time-per-phase"],
  ipv6: ["monitors", "ipv6"],
  "response-checks": ["monitors", "response-checks-json"],
  "mcp-checks": ["monitors", "mcp-servers"],
  heartbeat: ["monitors", "heartbeat-monitors-cron-jobs"],
  "status-pages": ["status-pages"],
  maintenance: ["alerts", "maintenance-windows"],
  expiry: ["alerts", "expiry-alerts"],
  notifications: ["alerts"],
  sso: ["security", "single-sign-on"],
  "2fa": ["security", "two-factor-authentication"],
  api: ["integrations", "api-tokens"],
  mcp: ["integrations", "mcp-server"],
  apps: ["ios-app"],
  prometheus: ["integrations", "prometheus"],
  upgrade: ["administration", "upgrade"],
  "usage-stats": ["administration", "usage-stats"],
  verify: ["administration", "verify-a-release"],
  build: ["install", "without-docker"],
}

export function oldAnchorMap(lang: Lang) {
  return Object.fromEntries(
    Object.entries(OLD_ANCHORS).map(([old, [slug, id]]) => [old, docHref(lang, slug, id)]),
  )
}

// The search index of a language, served as /docs-search.<lang>.json and
// loaded by the sidebar's search box on first use: one entry per page and
// per section, with its text as plain words.
export type SearchEntry = { page: string; title: string; href: string; text: string }

export function searchIndex(lang: Lang): SearchEntry[] {
  const out: SearchEntry[] = []
  for (const d of allDocs(lang)) {
    const page = localePath(lang, d.slug ? `/docs/${d.slug}/` : "/docs/")
    const seen = new Map<string, number>()
    let cur: SearchEntry = { page: d.title, title: d.title, href: page, text: "" }
    out.push(cur)
    for (const t of d.tokens) {
      if (t.type === "heading") {
        const text = plain((t as Tokens.Heading).text)
        const id = slugify(text, seen)
        if (t.depth === 1) continue
        cur = { page: d.title, title: text, href: `${page}#${id}`, text: "" }
        out.push(cur)
      } else if (t.type !== "space" && t.type !== "code") {
        cur.text += " " + searchText(t.raw)
      }
    }
  }
  for (const e of out) e.text = e.text.replace(/\s+/g, " ").trim()
  return out
}

// Plain words of a Markdown block for the search index. Unlike plain(), "_"
// inside words stays (PINGTXT_DB_URL).
function searchText(md: string) {
  return md
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/^[|:\-\s]+$/gm, "")
    .replace(/<[^>]+>/g, "")
    .replace(/[`*|>#]/g, " ")
    .replace(/(^|\s)_+|_+(?=\s|$|[.,;:])/g, "$1")
}
