import Link from "next/link"

import { SiteHeader } from "@/components/site-header"
import { StatusLine, STATUS_URL } from "@/components/status-dot"
import { docHref } from "@/lib/docs"
import { localePath, tx, type Lang } from "@/lib/i18n"
import { REPO, WRAP } from "@/components/ui"

export { Eyebrow, Prompt, REPO, SectionTitle, WRAP } from "@/components/ui"
const TELEMETRY = "https://telemetry.pingtxt.com/"


const footLink = "underline decoration-foreground/30 underline-offset-4 hover:decoration-up hover:decoration-2"

export function SiteShell({ lang, notFound, children }: { lang: Lang; notFound?: boolean; children: React.ReactNode }) {
  const t = tx(lang)
  const p = (path: string) => localePath(lang, path)
  const nav = [
    { href: p("/#features"), label: t("features", "funzioni") },
    { href: p("/#install"), label: t("install", "installa") },
    { href: p("/docs/"), label: t("docs", "docs") },
    { href: p("/docs/api/"), label: "api" },
    { href: REPO, label: "github", external: true },
  ]
  const columns: { title: string; links: { href: string; label: string }[] }[] = [
    {
      title: t("project", "progetto"),
      links: [
        { href: REPO, label: t("Source on GitHub", "Sorgente su GitHub") },
        { href: `${REPO}/releases`, label: t("Releases", "Release") },
        { href: `${REPO}/issues`, label: t("Issues", "Issue") },
        { href: `${REPO}/blob/main/CONTRIBUTING.md`, label: t("Contributing", "Contribuire") },
        { href: `${REPO}/blob/main/SECURITY.md`, label: t("Security", "Sicurezza") },
        { href: `${REPO}/blob/main/LICENSE`, label: t("AGPL-3.0 license", "Licenza AGPL-3.0") },
      ],
    },
    {
      title: "docs",
      links: [
        { href: docHref(lang, ""), label: t("Introduction", "Introduzione") },
        { href: docHref(lang, "install"), label: t("Install", "Installazione") },
        { href: docHref(lang, "locations"), label: t("Locations", "Località") },
        { href: docHref(lang, "status-pages"), label: "Status page" },
        { href: p("/docs/api/"), label: "REST API" },
        { href: "/llms.txt", label: "llms.txt" },
      ],
    },
    {
      title: t("around", "dintorni"),
      links: [
        { href: STATUS_URL, label: t("Service status", "Stato dei servizi") },
        { href: TELEMETRY, label: t("Usage stats", "Statistiche d'uso") },
        { href: docHref(lang, "ios-app"), label: t("iOS app", "App iOS") },
        { href: p("/privacy/"), label: t("Privacy policy", "Privacy") },
      ],
    },
  ]
  return (
    <div className="group/site flex min-h-svh flex-col">
      <SiteHeader nav={nav} switcher={!notFound} />
      <main className={`flex-1 has-[.site-bleed]:max-w-none has-[.site-bleed]:px-0 ${WRAP}`}>{children}</main>
      <footer className="mt-20 group-has-[.site-bleed]/site:mt-0">
        <div aria-hidden className="pixel-edge bg-band" />
        <div className="bg-band">
          <div className={`${WRAP} grid gap-10 py-12 sm:grid-cols-2 lg:grid-cols-[1.4fr_1fr_1fr_1fr]`}>
            <div className="space-y-4">
              <Link href={p("/")} className="text-xl font-bold tracking-tight">
                <span className="text-up">&gt;</span> pingtxt<span className="cursor-blink text-up">_</span>
              </Link>
              <p className="max-w-xs text-muted-foreground">
                {t("Open source under ", "Open source con licenza ")}
                <a href={`${REPO}/blob/main/LICENSE`} className={footLink}>
                  AGPL-3.0
                </a>
                {t(
                  ". Self-hosted uptime monitoring, built in the open.",
                  ". Monitoraggio dell'uptime self-hosted, sviluppato alla luce del sole.",
                )}
              </p>
              <div className="text-xs text-muted-foreground">
                <StatusLine />
              </div>
            </div>
            {columns.map((c) => (
              <div key={c.title}>
                <div className="text-[11px] font-bold uppercase tracking-widest">{c.title}</div>
                <ul className="mt-4 space-y-2">
                  {c.links.map((l) => (
                    <li key={l.href}>
                      <a href={l.href} className={footLink}>
                        {l.label}
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
          <div className="border-t border-edge">
            <div
              className={`${WRAP} flex flex-wrap items-center justify-between gap-x-6 gap-y-1 py-3 text-[11px] font-bold uppercase tracking-widest`}
            >
              <span>pingtxt.com</span>
              <span className="font-normal normal-case tracking-normal text-muted-foreground max-sm:hidden">
                {t("press", "premi")} <kbd className="border border-current/40 px-1">d</kbd>{" "}
                {t("to toggle theme", "per cambiare tema")}
              </span>
              <span>{t("uptime, in plain text", "uptime, in testo semplice")}</span>
            </div>
          </div>
        </div>
      </footer>
    </div>
  )
}
