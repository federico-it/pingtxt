import Link from "next/link"

import { DocsNav, type NavGroup } from "@/components/docs-nav"
import { DocsToc, type TocItem } from "@/components/docs-toc"
import { REPO } from "@/components/site-shell"
import { DOC_GROUPS, docHref, loadDoc } from "@/lib/docs"
import { localePath, tx, type Lang } from "@/lib/i18n"

// The frame of every docs page, the Markdown ones and the API reference:
// sidebar with search on the left, the page, "on this page" on the right.

function groups(lang: Lang): NavGroup[] {
  const t = tx(lang)
  const page = (s: Parameters<typeof loadDoc>[1]) => ({ href: docHref(lang, s), label: loadDoc(lang, s).title })
  return [
    ...DOC_GROUPS.map((g, i) => ({
      label: t(g.en, g.it),
      items: [...(i === 0 ? [{ href: docHref(lang, ""), label: t("Overview", "Panoramica") }] : []), ...g.pages.map(page)],
    })),
    {
      label: t("Reference", "Riferimento"),
      items: [{ href: localePath(lang, "/docs/api/"), label: t("REST API", "REST API") }],
    },
  ]
}

export type PageLink = { href: string; title: string }

export function DocsFrame({
  lang,
  toc,
  prev,
  next,
  source,
  children,
}: {
  lang: Lang
  toc: TocItem[]
  prev?: PageLink
  next?: PageLink
  // The file in the repository the page comes from, for the edit line.
  source: { path: string; edit: boolean }
  children: React.ReactNode
}) {
  const t = tx(lang)
  const link = "text-foreground underline decoration-up underline-offset-4 hover:decoration-2"
  return (
    <div className="docs-frame grid gap-x-10 pt-6 lg:grid-cols-[250px_minmax(0,1fr)] lg:pt-0 xl:grid-cols-[250px_minmax(0,1fr)_190px]">
      <DocsNav title={t("pingtxt manual", "manuale di pingtxt")} groups={groups(lang)} />
      <article className="min-w-0 max-w-[46rem] py-8 lg:py-12">
        {children}
        <nav
          aria-label={t("previous and next page", "pagina precedente e successiva")}
          className="mt-14 grid grid-cols-2 gap-4 border-t pt-6"
        >
          {prev ? <Neighbour dir="prev" lang={lang} page={prev} /> : <span />}
          {next ? <Neighbour dir="next" lang={lang} page={next} /> : <span />}
        </nav>
        <p className="mt-8 text-xs leading-relaxed text-muted-foreground">
          {t("This page lives in the pingtxt repository as ", "Questa pagina vive nel repository di pingtxt come ")}
          <a href={`${REPO}/blob/main/${source.path}`} className={link}>
            {source.path}
          </a>
          .{" "}
          {source.edit && (
            <>
              <a href={`${REPO}/edit/main/${source.path}`} className={link}>
                {t("Suggest an edit", "Suggerisci una modifica")}
              </a>
              .{" "}
            </>
          )}
          {t("All the docs in one Markdown file: ", "Tutta la documentazione (in inglese) in un solo file Markdown: ")}
          <Link href="/llms-full.txt" prefetch={false} className={link}>
            llms-full.txt
          </Link>
          .
        </p>
      </article>
      <DocsToc items={toc} />
    </div>
  )
}

function Neighbour({ dir, lang, page }: { dir: "prev" | "next"; lang: Lang; page: PageLink }) {
  const t = tx(lang)
  return (
    <Link
      href={page.href}
      className={`group border bg-background px-4 py-3 shadow-[3px_3px_0_0_var(--color-up)] transition-all hover:-translate-x-px hover:-translate-y-px hover:shadow-[4px_4px_0_0_var(--color-up)] ${
        dir === "next" ? "col-start-2 text-right" : ""
      }`}
    >
      <span className="block text-[11px] uppercase tracking-wider text-muted-foreground">
        {dir === "prev" ? `← ${t("previous", "precedente")}` : `${t("next", "successiva")} →`}
      </span>
      <span className="mt-0.5 block font-semibold">{page.title}</span>
    </Link>
  )
}

// A section heading of a docs page: "##" or "###" in front, which links to it.
export function DocHeading({ depth, id, children }: { depth: number; id: string; children: React.ReactNode }) {
  const Tag = depth === 2 ? "h2" : "h3"
  return (
    <Tag id={id} className={`group scroll-mt-20 font-bold ${depth === 2 ? "pt-8 text-lg" : "pt-3 text-[15px]"}`}>
      <a href={`#${id}`} aria-hidden tabIndex={-1} className="mr-2 font-normal text-up/60 no-underline! group-hover:text-up">
        {"#".repeat(depth)}
      </a>
      {children}
    </Tag>
  )
}
