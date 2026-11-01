import Link from "next/link"

import { SiteHeader } from "@/components/site-header"
import { StatusLine } from "@/components/status-dot"
import { localePath, tx, type Lang } from "@/lib/i18n"

export const REPO = "https://github.com/federico-it/pingtxt"

const footLink = "underline-offset-4 hover:text-foreground hover:underline"

export function SiteShell({ lang, notFound, children }: { lang: Lang; notFound?: boolean; children: React.ReactNode }) {
  const t = tx(lang)
  const p = (path: string) => localePath(lang, path)
  const nav = [
    { href: p("/#features"), label: t("features", "funzioni") },
    { href: p("/#install"), label: t("install", "installa") },
    { href: p("/docs/"), label: t("docs", "docs") },
    { href: p("/docs/api/"), label: "api" },
    { href: REPO, label: "github" },
  ]
  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col px-4 sm:px-6">
      <SiteHeader nav={nav} switcher={!notFound} />
      <main className="flex-1">{children}</main>
      <footer className="mt-16 flex flex-wrap items-center justify-between gap-2 border-t border-dashed py-4 text-xs text-muted-foreground">
        <span>
          pingtxt ·{" "}
          <a href={`${REPO}/blob/main/LICENSE`} className={footLink}>
            {t("AGPL-3.0 license", "licenza AGPL-3.0")}
          </a>{" "}
          ·{" "}
          <a href={REPO} className={footLink}>
            github.com/federico-it/pingtxt
          </a>{" "}
          ·{" "}
          <Link href={p("/docs/#contributing")} className={footLink}>
            {t("contributing", "contribuire")}
          </Link>{" "}
          ·{" "}
          <a href={`${REPO}/blob/main/SECURITY.md`} className={footLink}>
            {t("security", "sicurezza")}
          </a>{" "}
          ·{" "}
          <Link href={p("/privacy/")} className={footLink}>
            privacy
          </Link>
        </span>
        <StatusLine />
        <span>
          {t("press", "premi")} <kbd className="border px-1">d</kbd> {t("to toggle theme", "per cambiare tema")}
        </span>
      </footer>
    </div>
  )
}

export function Prompt({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-xs text-muted-foreground">
      <span className="text-up">$</span> {children}
    </div>
  )
}

export function SectionTitle({ id, children }: { id?: string; children: React.ReactNode }) {
  return (
    <h2 id={id} className="flex items-center gap-3 text-[11px] uppercase tracking-wider text-muted-foreground">
      <span>──</span>
      <span>{children}</span>
      <span className="h-px flex-1 border-t border-dashed" />
    </h2>
  )
}
