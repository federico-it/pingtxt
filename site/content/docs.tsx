import type { Metadata } from "next"
import { CopyBlock } from "@/components/copy-block"
import { Prompt, REPO } from "@/components/site-shell"
import { DocHeading, DocsFrame } from "@/content/docs-frame"
import {
  blockHtml,
  inlineHtml,
  checkDocs,
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
  "space-y-5 text-[14px] leading-7 [&_a]:underline [&_a]:decoration-up [&_a]:underline-offset-4 [&_a:hover]:decoration-2 " +
  "[&_strong]:font-semibold [&_code]:bg-muted [&_code]:px-1 [&_code]:py-px [&_code]:text-[12.5px] [&_code]:text-up " +
  "[&_kbd]:border [&_kbd]:px-1 [&_kbd]:text-[12px] [&_li]:ml-5 [&_li]:pl-1 [&_ul>li]:list-['–'] [&_ul>li]:marker:text-muted-foreground " +
  "[&_ol>li]:list-decimal [&_ol>li]:marker:text-muted-foreground [&_ol]:space-y-1.5 [&_ul]:space-y-1.5 [&_em]:italic"

const table =
  "overflow-x-auto [&_table]:w-full [&_table]:border-collapse [&_table]:border [&_table]:text-left [&_table]:text-[12.5px] [&_table]:leading-relaxed " +
  "[&_th]:border [&_th]:bg-muted [&_th]:px-3 [&_th]:py-2 [&_th]:text-[11px] [&_th]:font-semibold [&_th]:uppercase [&_th]:tracking-wider " +
  "[&_td]:border [&_td]:px-3 [&_td]:py-2 [&_td]:align-top [&_td:first-child_code]:whitespace-nowrap"

function Body({ doc }: { doc: Doc }) {
  const seen = new Map<string, number>()
  const out: React.ReactNode[] = []
  let lead = true
  doc.tokens.forEach((tok, i) => {
    if (tok.type === "heading") {
      const id = slugify(plain(tok.text), seen)
      if (tok.depth === 1) return
      lead = false
      out.push(
        <DocHeading key={i} depth={tok.depth} id={id}>
          <span dangerouslySetInnerHTML={{ __html: inlineHtml(doc.lang, tok.text) }} />
        </DocHeading>,
      )
      return
    }
    if (tok.type === "code") {
      const label = tok.lang && !["text", "sh"].includes(tok.lang) ? tok.lang : undefined
      out.push(<CopyBlock key={i} code={tok.text} label={label} />)
      return
    }
    if (tok.type === "space") return
    // The first paragraph is the page's lead.
    const isLead = lead && tok.type === "paragraph"
    lead = false
    out.push(
      <div
        key={i}
        className={tok.type === "table" ? table : isLead ? "text-[15px] text-muted-foreground" : undefined}
        dangerouslySetInnerHTML={{ __html: blockHtml(doc.lang, tok) }}
      />,
    )
  })
  return <div className={prose}>{out}</div>
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
  const page = (s: DocSlug | "") => ({
    href: docHref(lang, s),
    title: s ? loadDoc(lang, s).title : t("Overview", "Panoramica"),
  })
  return (
    <DocsFrame
      lang={lang}
      toc={doc.headings.filter((h) => h.depth === 2 || h.depth === 3)}
      prev={prev === undefined ? undefined : page(prev)}
      next={next === undefined ? { href: localePath(lang, "/docs/api/"), title: t("REST API", "REST API") } : page(next)}
      source={{ path: `docs/${lang}/${doc.file}`, edit: true }}
    >
      {!slug && <OldAnchors lang={lang} />}
      <header className="border-b pb-5">
        <Prompt>man pingtxt{slug ? `-${slug}` : ""}</Prompt>
        <h1 className="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">{doc.title}</h1>
      </header>
      <div className="mt-8">
        <Body doc={doc} />
      </div>
    </DocsFrame>
  )
}
