"use client"

import { Fragment, useEffect, useSyncExternalStore } from "react"
import useSWR from "swr"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { StatusText } from "@/components/status"
import { LanguagePicker } from "@/components/language-picker"
import { ThemeToggle } from "@/components/theme-toggle"
import { UptimeBars } from "@/components/uptime-bars"
import { api, ApiError, type PageStatus, type PublicStatusPage } from "@/lib/api"
import { useNow } from "@/hooks/use-now"
import { ago, datetime, duration, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

// Texts are in statusPublic.banner.<status>.
const banner: Record<PageStatus, { cls: string; glyph: string }> = {
  operational: { cls: "border-up/40 text-up", glyph: "●" },
  degraded: { cls: "border-warn/40 text-warn", glyph: "◐" },
  partial_outage: { cls: "border-down/40 text-down", glyph: "●" },
  major_outage: { cls: "border-down/40 text-down", glyph: "●" },
  unknown: { cls: "text-muted-foreground", glyph: "◌" },
}

const noop = () => () => {}

/**
 * The page is served at /status/<slug>/ on the hub, at / on a custom domain
 * (the hub resolves the page from the host) and at /status/?slug= in dev.
 */
function endpoint(): string {
  const m = window.location.pathname.match(/^\/status\/([a-z0-9-]+)\/?$/)
  const slug = m?.[1] ?? new URLSearchParams(window.location.search).get("slug")
  return slug ? `/api/public/status/${encodeURIComponent(slug)}` : "/api/public/status"
}

export default function PublicStatusPage() {
  const path = useSyncExternalStore(noop, endpoint, () => null)
  const now = useNow()
  const t = useTranslations("statusPublic")
  const tc = useTranslations("common")
  const { data, error } = useSWR<PublicStatusPage>(path, (p: string) => api<PublicStatusPage>(p), {
    refreshInterval: 60_000,
  })

  useEffect(() => {
    if (data) document.title = data.title
  }, [data])

  if (error) {
    return (
      <div className="flex min-h-svh flex-col items-center justify-center gap-2 px-4 text-center">
        <div className="text-xs text-muted-foreground">
          <span className="text-primary">$</span> curl status
        </div>
        <div>{error instanceof ApiError && error.status === 404 ? t("notFound") : `! ${error.message}`}</div>
      </div>
    )
  }
  if (!data) {
    return (
      <div className="flex min-h-svh items-center justify-center text-muted-foreground">
        <span>
          {tc("loading")}<span className="cursor-blink">_</span>
        </span>
      </div>
    )
  }

  const status = banner[data.status] ? data.status : "unknown"
  const b = banner[status]
  const open = data.incidents.filter((i) => !i.resolved_at)
  const past = data.incidents.filter((i) => i.resolved_at)

  return (
    <div className="mx-auto flex min-h-svh w-full max-w-3xl flex-col px-4 sm:px-6">
      <header className="flex items-center gap-4 border-b border-dashed py-3">
        <h1 className="min-w-0 truncate font-semibold tracking-tight">
          <span className="text-primary">&gt;</span> {data.title}
        </h1>
        {!data.published && <span className="text-xs text-warn">[{t("draft")}]</span>}
        <div className="ml-auto flex items-center gap-1">
          <LanguagePicker />
          <ThemeToggle />
        </div>
      </header>

      <main className="flex-1 py-8">
        <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-1 border px-4 py-4", b.cls)}>
          <span className={cn("text-lg", data.status.endsWith("outage") && "animate-pulse")}>{b.glyph}</span>
          <span className="text-base font-medium">{t(`banner.${status}`)}</span>
          <span className="ml-auto text-xs text-muted-foreground">{now ? t("updated", { ago: ago(data.updated_at) }) : ""}</span>
        </div>
        {data.description && <p className="mt-4 whitespace-pre-line text-muted-foreground">{data.description}</p>}

        {open.length > 0 && (
          <Section title={t("ongoingIncidents")}>
            <div className="border-t border-dashed">
              {open.map((i) => (
                <div key={i.id} className="flex flex-wrap gap-x-4 border-b border-dashed px-2 py-2">
                  <span className="animate-pulse text-down">{t("ongoing")}</span>
                  <span className="font-medium">{i.monitor_name}</span>
                  <span className="ml-auto text-muted-foreground">
                    {t("since", { start: datetime(i.started_at), duration: duration((now || data.updated_at) - i.started_at) })}
                  </span>
                </div>
              ))}
            </div>
          </Section>
        )}

        <Section title={t("services", { days: data.days })}>
          {data.monitors.length === 0 ? (
            <div className="border border-dashed p-8 text-center text-muted-foreground">{t("nothing")}</div>
          ) : (
            <div className="border-t border-dashed">
              {data.monitors.map((m, i) => (
                <Fragment key={m.id}>
                  {m.group && m.group !== data.monitors[i - 1]?.group && <GroupHeader page={data} name={m.group} />}
                  <div className={cn("grid gap-2 border-b border-dashed px-2 py-3", m.group && "pl-4")}>
                  <div className="flex items-center gap-3">
                    <span className="min-w-0 flex-1 truncate font-medium">{m.name}</span>
                    <span className="text-xs text-muted-foreground">{pct(m.uptime)}</span>
                    <StatusText status={m.status} className="w-24 justify-end text-xs" />
                  </div>
                    <UptimeBars bars={m.bars} daily height="h-7" />
                  </div>
                </Fragment>
              ))}
              <div className="flex justify-between px-2 pt-1.5 text-[11px] text-muted-foreground">
                <span>{t("daysAgo", { days: data.days })}</span>
                <span>{t("today")}</span>
              </div>
            </div>
          )}
        </Section>

        {data.show_incidents && (
          <Section title={t("pastIncidents")}>
            {past.length === 0 ? (
              <div className="text-xs text-muted-foreground">{t("noIncidents", { days: data.days })}</div>
            ) : (
              <div className="border-t border-dashed">
                {past.map((i) => (
                  <div key={i.id} className="flex flex-wrap gap-x-4 border-b border-dashed px-2 py-2">
                    <span className="text-up">{t("resolved")}</span>
                    <span className="font-medium">{i.monitor_name}</span>
                    <span className="ml-auto text-muted-foreground">
                      {t("downFor", { start: datetime(i.started_at), duration: duration(i.resolved_at - i.started_at) })}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </Section>
        )}
      </main>

      <footer className="flex justify-between border-t border-dashed py-3 text-xs text-muted-foreground">
        <span>{t.rich("poweredBy", { hl: (c) => <span className="text-foreground">{c}</span> })}</span>
        <span>{t("refreshes")}</span>
      </footer>
    </div>
  )
}

/** Section title of a group, with the state of its worst monitor. */
function GroupHeader({ page, name }: { page: PublicStatusPage; name: string }) {
  const t = useTranslations("statusPublic.group")
  const ms = page.monitors.filter((m) => m.group === name && m.status !== "paused")
  const down = ms.filter((m) => m.status === "down").length
  const degraded = ms.filter((m) => m.status === "degraded").length
  let text = ""
  let cls = "text-muted-foreground"
  if (down > 0) [text, cls] = [down === ms.length ? t("outage") : t("down", { down, total: ms.length }), "text-down"]
  else if (degraded > 0) [text, cls] = [t("degraded"), "text-warn"]
  else if (ms.some((m) => m.status === "up")) [text, cls] = [t("operational"), "text-up"]
  return (
    <div className="flex items-center gap-3 border-b border-dashed bg-muted/40 px-2 py-1.5 text-xs">
      <span className="font-medium uppercase">{name}</span>
      <span className={cn("ml-auto", cls)}>{text}</span>
    </div>
  )
}
