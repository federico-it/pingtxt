"use client"

import Link from "next/link"
import { useState } from "react"
import { useTranslations } from "use-intl"

import { CopyBlock } from "@/components/copy-block"
import { Section } from "@/components/page-header"
import { useApi, type StatusPage } from "@/lib/api"
import { cn } from "@/lib/utils"

const kinds = ["status", "uptime", "response"] as const
const periods = ["24h", "7d", "30d", "90d"] as const
const formats = ["markdown", "html", "url"] as const
type Kind = (typeof kinds)[number]
type Period = (typeof periods)[number]

// Same request (and cache) as the status pages list. hub_url follows the
// public URL setting, so snippets work behind a reverse proxy.
const usePages = () => useApi<{ pages: StatusPage[]; hub_url: string }>("/api/status-pages", { refreshInterval: 0 })

/** Where a page is reachable: its custom domain, or /status/<slug>/ here. */
function pageBase(p: Pick<StatusPage, "slug" | "domain">, origin: string) {
  return p.domain
    ? { site: `https://${p.domain}`, page: `https://${p.domain}/` }
    : { site: origin, page: `${origin}/status/${p.slug}/` }
}

function Tabs<T extends string>({ items, value, onChange, label }: { items: readonly T[]; value: T; onChange: (v: T) => void; label: (v: T) => string }) {
  return (
    <div className="flex flex-wrap gap-1 text-xs">
      {items.map((k) => (
        <button
          key={k}
          type="button"
          onClick={() => onChange(k)}
          className={cn("px-1.5 text-muted-foreground hover:text-foreground", value === k && "bg-accent text-foreground")}
        >
          {label(k)}
        </button>
      ))}
    </div>
  )
}

function Snippet({ page, path, alt, origin }: { page: StatusPage; path: string; alt: string; origin: string }) {
  const [format, setFormat] = useState<(typeof formats)[number]>("markdown")
  const t = useTranslations("badges")
  const base = pageBase(page, origin)
  const src = `${base.site}${path}`
  const code = {
    markdown: `[![${alt}](${src})](${base.page})`,
    html: `<a href="${base.page}"><img src="${src}" alt="${alt}"></a>`,
    url: src,
  }[format]
  return (
    <div className="grid gap-2">
      <div className="flex min-h-5 flex-wrap items-center justify-between gap-2">
        {/* eslint-disable-next-line @next/next/no-img-element -- live SVG from the hub */}
        <img src={path} alt={alt} className="h-5" />
        <Tabs items={formats} value={format} onChange={setFormat} label={(f) => t(`formats.${f}`)} />
      </div>
      <CopyBlock code={code} />
    </div>
  )
}

/** README badges of a monitor, served through the published pages that show it. */
export function MonitorBadges({ monitorId, groupId }: { monitorId: number; groupId: number }) {
  const t = useTranslations("badges")
  const list = usePages()
  const [kind, setKind] = useState<Kind>("uptime")
  const [period, setPeriod] = useState<Period>("30d")
  const [slug, setSlug] = useState("")
  if (!list.data) return null
  const shown = list.data.pages.filter(
    (p) => p.published && (p.monitor_ids.includes(monitorId) || (groupId > 0 && (p.group_ids ?? []).includes(groupId)))
  )
  // Open pages first: badges of a password protected page need the password.
  shown.sort((a, b) => Number(a.protected) - Number(b.protected))
  const page = shown.find((p) => p.slug === slug) ?? shown[0]
  const origin = list.data.hub_url
  const p = kind === "status" ? "" : `?period=${period}`
  return (
    <Section
      title={t("title")}
      actions={<Tabs items={kinds} value={kind} onChange={setKind} label={(k) => t(`kinds.${k}`)} />}
    >
      {!page ? (
        <p className="text-xs text-muted-foreground">
          {t.rich("needsPage", {
            link: (c) => (
              <Link href="/status-pages/" className="underline underline-offset-4 hover:text-foreground">
                {c}
              </Link>
            ),
          })}
        </p>
      ) : (
        <>
          <p className="mb-2 text-xs text-muted-foreground">{t("hint", { page: page.title })}</p>
          {page.protected && <p className="mb-2 text-xs text-warn">{t("protectedHint")}</p>}
          <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1">
            {shown.length > 1 && (
              <Tabs items={shown.map((s) => s.slug)} value={page.slug} onChange={setSlug} label={(s) => shown.find((x) => x.slug === s)!.title} />
            )}
            {kind !== "status" && <Tabs items={periods} value={period} onChange={setPeriod} label={(x) => x} />}
          </div>
          <Snippet
            key={`${page.slug}/${kind}`}
            page={page}
            path={`/api/badge/${page.slug}/${monitorId}/${kind}.svg${p}`}
            alt={t(`kinds.${kind}`)}
            origin={origin}
          />
        </>
      )}
    </Section>
  )
}

/** README badge with the overall state of a status page. */
export function PageBadge({ page }: { page: StatusPage }) {
  const t = useTranslations("badges")
  const list = usePages()
  if (!list.data) return null
  return (
    <Section title={t("title")}>
      <p className="mb-2 text-xs text-muted-foreground">{t("pageHint")}</p>
      {page.protected && <p className="mb-2 text-xs text-warn">{t("protectedHint")}</p>}
      <Snippet page={page} path={`/api/badge/${page.slug}/status.svg`} alt={t("kinds.status")} origin={list.data.hub_url} />
    </Section>
  )
}
