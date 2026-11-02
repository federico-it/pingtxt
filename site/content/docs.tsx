import type { Metadata } from "next"
import Link from "next/link"

import { CopyBlock } from "@/components/copy-block"
import { Prompt, REPO, SectionTitle } from "@/components/site-shell"
import {
  blockHtml,
  inlineHtml,
  checkDocs,
  DOC_PAGES,
  docHref,
  loadDoc,
  neighbours,
  oldAnchorMap,
  slugify,
  plain,
  type Doc,
  type DocSlug,
} from "@/lib/docs"
import { localePath, tx, type Lang } from "@/lib/i18n"
import { pageMeta } from "@/lib/seo"

// The docs pages render the Markdown files in the repository's docs/ folder
// (see lib/docs.ts). Only the frame around them is written here.

export function docsMeta(lang: Lang, slug: DocSlug | ""): Metadata {
  checkDocs()
  const doc = loadDoc(lang, slug)
  const t = tx(lang)
  return pageMeta({
    lang,
    title: slug ? `${doc.title} · ${t("docs", "documentazione")}` : t("docs", "documentazione"),
    description: doc.description,
    path: slug ? `/docs/${slug}/` : "/docs/",
  })
}

const prose =
  "space-y-4 text-muted-foreground [&_a]:text-foreground [&_a]:underline [&_a]:underline-offset-4 " +
  "[&_strong]:text-foreground [&_strong]:font-semibold [&_code]:bg-muted [&_code]:px-1 [&_code]:text-foreground " +
  "[&_kbd]:border [&_kbd]:px-1 [&_kbd]:text-foreground [&_li]:ml-4 [&_ul>li]:list-['-_'] [&_ol>li]:list-decimal " +
  "[&_ol]:ml-4 [&_ol]:space-y-1 [&_ul]:space-y-1 [&_em]:italic"

const table =
  "overflow-x-auto [&_table]:w-full [&_table]:text-left [&_table]:text-[12px] [&_th]:border-b [&_th]:border-dashed " +
  "[&_th]:px-2 [&_th]:py-1.5 [&_th]:text-[11px] [&_th]:font-normal [&_th]:uppercase [&_th]:tracking-wider " +
  "[&_td]:border-b [&_td]:border-dashed [&_td]:px-2 [&_td]:py-1.5 [&_td]:align-top " +
  "[&_td:first-child]:text-foreground [&_td:first-child_code]:whitespace-nowrap"

function Body({ doc }: { doc: Doc }) {
  const seen = new Map<string, number>()
  const out: React.ReactNode[] = []
  doc.tokens.forEach((tok, i) => {
    if (tok.type === "heading") {
      const id = slugify(plain(tok.text), seen)
      if (tok.depth === 1) return
      const html = inlineHtml(doc.lang, tok.text)
      if (tok.depth === 2) {
        out.push(
          <div key={i} className="pt-6">
            <SectionTitle id={id}>
              <span dangerouslySetInnerHTML={{ __html: html }} />
            </SectionTitle>
          </div>,
        )
      } else {
        out.push(
          <h3
            key={i}
            id={id}
            className="scroll-mt-20 pt-2 font-semibold text-foreground"
            dangerouslySetInnerHTML={{ __html: html }}
          />,
        )
      }
      return
    }
    if (tok.type === "code") {
      const label = tok.lang && !["text", "sh"].includes(tok.lang) ? tok.lang : undefined
      out.push(<CopyBlock key={i} code={tok.text} label={label} />)
      return
    }
    if (tok.type === "space") return
    out.push(
      <div
        key={i}
        className={tok.type === "table" ? table : undefined}
        dangerouslySetInnerHTML={{ __html: blockHtml(doc.lang, tok) }}
      />,
    )
  })
  return <div className={prose}>{out}</div>
}

function Sidebar({ lang, current }: { lang: Lang; current: Doc }) {
  const t = tx(lang)
  const item = (slug: DocSlug | "", label: string) => {
    const active = slug === current.slug
    return (
      <li key={slug || "index"}>
        <Link
          href={docHref(lang, slug)}
          className={active ? "text-foreground" : "text-muted-foreground transition-colors hover:text-foreground"}
        >
          {active ? "> " : ""}
          {label}
        </Link>
        {active && slug && (
          <ul className="mt-1 mb-2 hidden space-y-0.5 border-l border-dashed pl-3 text-[12px] lg:block">
            {current.headings
              .filter((h) => h.depth === 2)
              .map((h) => (
                <li key={h.id}>
                  <a href={`#${h.id}`} className="text-muted-foreground transition-colors hover:text-foreground">
                    {h.text}
                  </a>
                </li>
              ))}
          </ul>
        )}
      </li>
    )
  }
  return (
    <aside className="lg:sticky lg:top-20 lg:max-h-[calc(100svh-6rem)] lg:self-start lg:overflow-y-auto">
      <div className="mb-2 text-[11px] uppercase tracking-wider text-muted-foreground">{t("docs", "documentazione")}</div>
      <ul className="flex flex-wrap gap-x-3 gap-y-0.5 lg:flex-col">
        {item("", t("Overview", "Panoramica"))}
        {DOC_PAGES.map((s) => item(s, loadDoc(lang, s).title))}
        <li>
          <Link
            href={localePath(lang, "/docs/api/")}
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            {t("REST API reference", "Riferimento API")}
          </Link>
        </li>
      </ul>
    </aside>
  )
}

// Old links to the single docs page (pingtxt.com/docs/#upgrade) jump to the
// page that has the section now. Static export: no server redirects.
function OldAnchors({ lang }: { lang: Lang }) {
  const map = { ...oldAnchorMap(lang), contributing: `${REPO}/blob/main/CONTRIBUTING.md` }
  const js = `(function(){var m=${JSON.stringify(map)};var h=location.hash.slice(1);if(m[h])location.replace(m[h]);})()`
  return <script dangerouslySetInnerHTML={{ __html: js }} />
}

export function DocsPage({ lang, slug }: { lang: Lang; slug: DocSlug | "" }) {
  checkDocs()
  const t = tx(lang)
  const doc = loadDoc(lang, slug)
  const { prev, next } = neighbours(slug)
  const link = (s: DocSlug | "" | undefined, dir: string) =>
    s === undefined ? (
      <span />
    ) : (
      <Link href={docHref(lang, s)} className="text-muted-foreground transition-colors hover:text-foreground">
        {dir === "prev" ? "← " : ""}
        {s ? loadDoc(lang, s).title : t("Overview", "Panoramica")}
        {dir === "next" ? " →" : ""}
      </Link>
    )
  return (
    <div className="grid gap-10 py-12 lg:grid-cols-[200px_1fr]">
      {!slug && <OldAnchors lang={lang} />}
      <Sidebar lang={lang} current={doc} />
      <article className="min-w-0 max-w-3xl space-y-6">
        <div className="space-y-2">
          <Prompt>man pingtxt{slug ? `-${slug}` : ""}</Prompt>
          <h1 className="text-xl font-semibold">{doc.title}</h1>
        </div>
        <Body doc={doc} />
        <nav className="flex flex-wrap justify-between gap-4 border-t border-dashed pt-4 text-sm">
          {link(prev, "prev")}
          {link(next, "next")}
        </nav>
        <p className="text-xs text-muted-foreground">
          <a
            href={`${REPO}/blob/main/docs/${lang}/${doc.file}`}
            className="underline-offset-4 hover:text-foreground hover:underline"
          >
            {t("edit this page on GitHub", "modifica questa pagina su GitHub")}
          </a>
        </p>
      </article>
    </div>
  )
}
