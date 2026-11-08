"use client"

import { Fragment, useEffect } from "react"
import { useTranslations } from "use-intl"

import "@/components/page-fonts"
import "@/components/status-page.css"

import { Section } from "@/components/page-header"
import { StatusText } from "@/components/status"
import { LanguagePicker } from "@/components/language-picker"
import { ThemeToggle } from "@/components/theme-toggle"
import { UptimeBars } from "@/components/uptime-bars"
import { reportImpactBorder, reportImpactText, ReportStatusText, ReportTimeline } from "@/components/report-timeline"
import { RichText, safeLink } from "@/components/rich-text"
import type { PageIncident, PageLink, PageLogo, PageStatus, PageTheme, PublicStatusPage } from "@/lib/api"
import { ago, datetime, duration, pct } from "@/lib/format"
import { customCSS, themeCSS } from "@/lib/page-theme"
import { cn } from "@/lib/utils"

// Texts are in statusPublic.banner.<status>.
const banner: Record<PageStatus, { cls: string; glyph: string }> = {
  operational: { cls: "border-up/40 text-up", glyph: "●" },
  degraded: { cls: "border-warn/40 text-warn", glyph: "◐" },
  partial_outage: { cls: "border-down/40 text-down", glyph: "●" },
  major_outage: { cls: "border-down/40 text-down", glyph: "●" },
  unknown: { cls: "text-muted-foreground", glyph: "◌" },
}

/**
 * A public status page as visitors see it. Also used by the editor to show a
 * live preview of unsaved settings (preview: no language/theme buttons).
 */
export function StatusPageView({
  data,
  now,
  notice,
  subscribe,
  preview = false,
  logoBase,
  feed,
}: {
  data: PublicStatusPage
  now: number
  notice?: React.ReactNode
  subscribe?: React.ReactNode
  preview?: boolean
  /** path of the logo files: the page JSON path + "/logo/" */
  logoBase?: string
  /** address of the page's Atom feed, linked in the footer */
  feed?: string
}) {
  const t = useTranslations("statusPublic")
  const status = banner[data.status] ? data.status : "unknown"
  const b = banner[status]
  const open = data.incidents.filter((i) => !i.resolved_at)
  const past = data.show_incidents ? data.incidents.filter((i) => i.resolved_at) : []
  const reports = data.reports ?? []
  const openReports = reports.filter((r) => !r.resolved_at)
  const pastReports = reports.filter((r) => r.resolved_at)

  const theme = data.theme
  const forced = theme?.mode ?? ""
  useForcedMode(preview ? "" : forced)
  useLogoFavicon(!preview && !!theme?.logo_favicon && data.logo && logoBase ? { logo: data.logo, base: logoBase } : null)
  const css = themeCSS(theme, preview ? ".pt-page" : ":root")
  const custom = customCSS(theme, preview)
  const container = "pt-container mx-auto w-full max-w-3xl px-4 sm:px-6"

  return (
    <div
      className={cn("pt-page flex w-full flex-col bg-background text-foreground", !preview && "min-h-svh", preview && forced === "dark" && "dark")}
      data-pt-style={theme?.style || undefined}
      data-pt-density={theme?.density || undefined}
      data-pt-width={theme?.width || undefined}
      data-pt-bars={theme?.bars || undefined}
      data-pt-header={theme?.header_style || undefined}
      data-pt-align={theme?.header_align || undefined}
    >
      {css && <style>{css}</style>}
      {custom && <style>{custom}</style>}
      <header className="pt-header">
        <div className={container}>
          <div className="pt-header-inner flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-dashed py-3">
            <div className="pt-brand flex min-w-0 flex-col">
              <h1 className="min-w-0 font-semibold tracking-tight">
                <PageBrand data={data} logoBase={logoBase} />
              </h1>
              {theme?.tagline && <p className="pt-tagline text-xs text-muted-foreground">{theme.tagline}</p>}
            </div>
            {!data.published && <span className="text-xs text-warn">[{t("draft")}]</span>}
            <div className="pt-header-tools ml-auto flex flex-wrap items-center gap-x-4 gap-y-1">
              {theme?.header_links?.length ? <PageLinks links={theme.header_links} className="pt-header-links" /> : null}
              {!preview && (
                <div className="flex items-center gap-1">
                  <LanguagePicker />
                  {!forced && <ThemeToggle />}
                </div>
              )}
            </div>
          </div>
        </div>
      </header>

      <main className={cn(container, "pt-main flex-1 py-8")}>
        {theme?.notice && (
          <div className="pt-notice pt-box mb-6 border-l-2 border-primary bg-muted/60 px-4 py-3">
            <RichText text={theme.notice} />
          </div>
        )}
        <div className={cn("pt-banner pt-box flex flex-wrap items-center gap-x-3 gap-y-1 border px-4 py-4", b.cls)}>
          <span className={cn("text-lg", data.status.endsWith("outage") && "animate-pulse")}>{b.glyph}</span>
          <span className="text-base font-medium">{t(`banner.${status}`)}</span>
          <span className="ml-auto text-xs text-muted-foreground">{now ? t("updated", { ago: ago(data.updated_at) }) : ""}</span>
        </div>
        {data.description && <p className="pt-description mt-4 whitespace-pre-line text-muted-foreground">{data.description}</p>}

        {notice}

        {openReports.length > 0 && (
          <div className="mt-6 grid gap-3">
            {openReports.map((r) => (
              <Report key={r.id} report={r} />
            ))}
          </div>
        )}

        {open.length > 0 && (
          <Section title={t("ongoingIncidents")} className="pt-section">
            <div className="pt-list border-t border-dashed">
              {open.map((i) => (
                <div key={i.id} className="pt-row flex flex-wrap gap-x-4 border-b border-dashed px-2 py-2">
                  <span className="animate-pulse text-down">{t("ongoing")}</span>
                  <span className="font-medium">{i.monitor_name}</span>
                  <span className="ml-auto text-muted-foreground">
                    {t("since", {
                      start: datetime(i.started_at),
                      duration: duration((now || data.updated_at) - i.started_at),
                    })}
                  </span>
                </div>
              ))}
            </div>
          </Section>
        )}

        <Section title={t("services", { days: data.days })} className="pt-section">
          {data.monitors.length === 0 ? (
            <div className="pt-box border border-dashed p-8 text-center text-muted-foreground">{t("nothing")}</div>
          ) : (
            <div className="pt-list border-t border-dashed">
              {data.monitors.map((m, i) => (
                <Fragment key={m.id}>
                  {m.group && m.group !== data.monitors[i - 1]?.group && <GroupHeader page={data} name={m.group} />}
                  <div className={cn("pt-monitor grid gap-2 border-b border-dashed px-2 py-3", m.group && "pl-4")}>
                    <div className="flex items-center gap-3">
                      <span className="min-w-0 flex-1 truncate font-medium">{m.name}</span>
                      <span className="text-xs text-muted-foreground">{pct(m.uptime)}</span>
                      <StatusText status={m.status} className="w-24 justify-end text-xs" />
                    </div>
                    <UptimeBars bars={m.bars} daily height="h-7" className="pt-bars" />
                  </div>
                </Fragment>
              ))}
              <div className="pt-axis flex justify-between px-2 pt-1.5 text-[11px] text-muted-foreground">
                <span>{t("daysAgo", { days: data.days })}</span>
                <span>{t("today")}</span>
              </div>
            </div>
          )}
        </Section>

        {(data.show_incidents || pastReports.length > 0) && (
          <Section title={t("pastIncidents")} className="pt-section">
            {past.length === 0 && pastReports.length === 0 ? (
              <div className="text-xs text-muted-foreground">{t("noIncidents", { days: data.days })}</div>
            ) : (
              <>
                {pastReports.length > 0 && (
                  <div className="pt-list border-t border-dashed">
                    {pastReports.map((r) => (
                      <details key={r.id} id={`incident-${r.id}`} className="pt-row group border-b border-dashed px-2 py-2">
                        <summary className="flex cursor-pointer list-none flex-wrap gap-x-4 [&::-webkit-details-marker]:hidden">
                          <span className="text-up">{t("resolved")}</span>
                          <span className="font-medium">{r.title}</span>
                          <span className="ml-auto text-muted-foreground">
                            {datetime(r.created_at)} · {duration(r.resolved_at - r.created_at)}
                            <span className="ml-2 inline-block transition-transform group-open:rotate-90">›</span>
                          </span>
                        </summary>
                        <div className="mt-3 pl-1">
                          <ReportTimeline incident={r} />
                        </div>
                      </details>
                    ))}
                  </div>
                )}
                {past.length > 0 && (
                  <div className={cn("pt-list border-t border-dashed", pastReports.length > 0 && "mt-4")}>
                    {past.map((i) => (
                      <div key={i.id} className="pt-row flex flex-wrap gap-x-4 border-b border-dashed px-2 py-2">
                        <span className="text-up">{t("resolved")}</span>
                        <span className="font-medium">{i.monitor_name}</span>
                        <span className="ml-auto text-muted-foreground">
                          {t("downFor", {
                            start: datetime(i.started_at),
                            duration: duration(i.resolved_at - i.started_at),
                          })}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </Section>
        )}

        {subscribe}
      </main>

      <footer className="pt-footer">
        <div className={container}>
          <div className="pt-footer-inner grid gap-2 border-t border-dashed py-3 text-xs text-muted-foreground">
            {theme?.footer_text && <RichText text={theme.footer_text} className="pt-footer-text" />}
            {theme?.footer_links?.length ? <PageLinks links={theme.footer_links} className="pt-footer-links" /> : null}
            <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
              {!theme?.hide_powered_by && (
                <a
                  href="https://pingtxt.com/?utm_source=status-page"
                  target="_blank"
                  rel="noopener"
                  className="pt-powered-by hover:underline"
                >
                  {t.rich("poweredBy", {
                    hl: (c) => <span className="text-foreground">{c}</span>,
                  })}
                </a>
              )}
              <span className={cn("flex flex-wrap gap-x-4", theme?.hide_powered_by && "ml-auto")}>
                {feed && (
                  <a href={feed} className="pt-feed hover:underline" type="application/atom+xml">
                    {t("feed")}
                  </a>
                )}
                <span>{t("refreshes")}</span>
              </span>
            </div>
          </div>
        </div>
      </footer>
    </div>
  )
}

/** Links set in the theme, for the header or the footer. */
function PageLinks({ links, className }: { links: PageLink[]; className?: string }) {
  return (
    <nav className={cn("flex flex-wrap items-center gap-x-4 gap-y-1", className)}>
      {links.map((l, i) => {
        const href = safeLink(l.url)
        return href ? (
          <a key={i} href={href} rel="noopener noreferrer" className="underline-offset-4 hover:underline">
            {l.label}
          </a>
        ) : null
      })}
    </nav>
  )
}

/** A forced light or dark theme switches the whole document while the page is open. */
export function useForcedMode(mode: PageTheme["mode"]) {
  useEffect(() => {
    if (!mode) return
    const html = document.documentElement
    const had = html.classList.contains("dark")
    const scheme = html.style.colorScheme
    html.classList.toggle("dark", mode === "dark")
    html.classList.toggle("light", mode === "light")
    html.style.colorScheme = mode
    return () => {
      html.classList.toggle("dark", had)
      html.classList.remove("light")
      html.style.colorScheme = scheme
    }
  }, [mode])
}

/** The page logo as the browser tab icon. */
function useLogoFavicon(v: { logo: PageLogo; base: string } | null) {
  const href = v ? `${v.base}logo.${v.logo.svg ? "svg" : "png"}?v=${v.logo.v}` : ""
  useEffect(() => {
    if (!href) return
    const links = Array.from(document.querySelectorAll<HTMLLinkElement>("link[rel~='icon']"))
    const old = links.map((l) => l.href)
    if (links.length === 0) {
      const l = document.createElement("link")
      l.rel = "icon"
      document.head.appendChild(l)
      links.push(l)
    }
    links.forEach((l) => (l.href = href))
    return () => links.forEach((l, i) => (old[i] ? (l.href = old[i]) : l.remove()))
  }, [href])
}

/** Logo and name of the page, a link when the page sets one. */
function PageBrand({ data, logoBase }: { data: PublicStatusPage; logoBase?: string }) {
  const logo = data.logo && logoBase ? <LogoImage logo={data.logo} base={logoBase} alt={data.title} /> : null
  const showTitle = !logo || !data.hide_title
  const inner = (
    <>
      {logo ?? <span className="text-primary">&gt;</span>}
      {showTitle && <span className="truncate">{data.title}</span>}
    </>
  )
  const cls = "flex min-w-0 items-center gap-2"
  if (data.header_url) {
    return (
      <a href={data.header_url} rel="noopener" className={cn(cls, "underline-offset-4 hover:underline")} title={data.header_url}>
        {inner}
      </a>
    )
  }
  return <span className={cls}>{inner}</span>
}

/** A vector logo as is; an image as AVIF or WebP, with PNG for old browsers. */
export function LogoImage({ logo, base, alt, className }: { logo: PageLogo; base: string; alt: string; className?: string }) {
  const v = `?v=${logo.v}`
  const size = logo.width > 0 && logo.height > 0 ? { width: logo.width, height: logo.height } : {}
  const cls = cn("h-8 w-auto max-w-[240px] shrink-0 object-contain object-left", className)
  if (logo.svg) {
    // eslint-disable-next-line @next/next/no-img-element
    return <img src={`${base}logo.svg${v}`} alt={alt} {...size} className={cls} />
  }
  return (
    <picture className="shrink-0">
      <source type="image/avif" srcSet={`${base}logo.avif${v}`} />
      <source type="image/webp" srcSet={`${base}logo.webp${v}`} />
      {}
      <img src={`${base}logo.png${v}`} alt={alt} {...size} className={cls} />
    </picture>
  )
}

/** Section title of a group, with the state of its worst monitor. A
 * subgroup's title is its path ("project › api"); a group's state sums up
 * its subgroups shown on the page too. */
function GroupHeader({ page, name }: { page: PublicStatusPage; name: string }) {
  const t = useTranslations("statusPublic.group")
  const ms = page.monitors.filter(
    (m) => (m.group === name || m.group?.startsWith(name + " › ")) && m.status !== "paused"
  )
  const down = ms.filter((m) => m.status === "down").length
  const degraded = ms.filter((m) => m.status === "degraded").length
  let text = ""
  let cls = "text-muted-foreground"
  if (down > 0) [text, cls] = [down === ms.length ? t("outage") : t("down", { down, total: ms.length }), "text-down"]
  else if (degraded > 0) [text, cls] = [t("degraded"), "text-warn"]
  else if (ms.some((m) => m.status === "up")) [text, cls] = [t("operational"), "text-up"]
  return (
    <div className="pt-group flex items-center gap-3 border-b border-dashed bg-muted/40 px-2 py-1.5 text-xs">
      <span className="font-medium uppercase">{name}</span>
      <span className={cn("ml-auto", cls)}>{text}</span>
    </div>
  )
}

/** An open incident written on the page, with its updates. */
function Report({ report }: { report: PageIncident }) {
  const t = useTranslations("statusPublic")
  const ti = useTranslations("pageIncidents.impact")
  return (
    <div id={`incident-${report.id}`} className={cn("pt-report pt-box border px-4 py-3", reportImpactBorder[report.impact])}>
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <ReportStatusText status={report.status} className="text-xs uppercase" />
        <span className="min-w-0 flex-1 font-medium">{report.title}</span>
        {report.impact !== "none" && <span className={cn("text-xs", reportImpactText[report.impact])}>[{ti(report.impact)}]</span>}
      </div>
      <div className="mt-1 text-xs text-muted-foreground">{t("reportStarted", { at: datetime(report.created_at) })}</div>
      <div className="mt-3">
        <ReportTimeline incident={report} />
      </div>
    </div>
  )
}
