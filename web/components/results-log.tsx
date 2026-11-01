"use client"

import { useState } from "react"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { phaseTitle } from "@/components/phase-chart"
import { OkText } from "@/components/status"
import { Button } from "@/components/ui/button"
import { api, useApi, type ResultRow } from "@/lib/api"
import { datetime, ms } from "@/lib/format"
import { segment } from "@/components/ui/control"

const pageSize = 50

type Page = { results: ResultRow[]; locations: Record<string, string> }

export interface TimeWindow {
  from: number
  to: number
}

/**
 * The check log of a monitor: newest first, with "load more", a filter for
 * failed checks and an optional time slot (a slot of the uptime bars).
 */
export function ResultsLog({
  monitorId,
  slot,
  onClearWindow,
  waitingText,
}: {
  monitorId: number
  slot: TimeWindow | null
  onClearWindow: () => void
  waitingText: string
}) {
  const t = useTranslations("resultsLog")
  const tm = useTranslations("monitor")
  const tp = useTranslations("phaseChart")
  const [failed, setFailed] = useState(false)
  const [more, setMore] = useState<ResultRow[]>([])
  const [end, setEnd] = useState(false)
  const [loading, setLoading] = useState(false)

  const params = new URLSearchParams({ limit: String(pageSize) })
  if (failed) params.set("failed", "1")
  if (slot) {
    params.set("from", String(slot.from))
    params.set("to", String(slot.to))
  }
  const key = `/api/monitors/${monitorId}/results?${params}`
  // Older pages are dropped when the filters change.
  const [lastKey, setLastKey] = useState(key)
  if (lastKey !== key) {
    setLastKey(key)
    setMore([])
    setEnd(false)
  }
  // Live updates only while the first page is shown alone: new rows would
  // otherwise leave a gap before the older pages.
  const first = useApi<Page>(key, {
    refreshInterval: slot || more.length > 0 ? 0 : 10_000,
  })
  const rows = [...(first.data?.results ?? []), ...more]
  const locations = first.data?.locations ?? {}
  const done = end || (first.data !== undefined && first.data.results.length < pageSize)

  async function loadMore() {
    const last = rows[rows.length - 1]
    if (!last) return
    setLoading(true)
    try {
      const p = new URLSearchParams(params)
      p.set("before", String(last.ts))
      const page = await api<Page>(`/api/monitors/${monitorId}/results?${p}`)
      setMore((m) => [...m, ...page.results])
      if (page.results.length < pageSize) setEnd(true)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Section
      id="log"
      title={tm("log")}
      actions={
        <span className="flex items-center gap-1 normal-case">
          {slot && (
            <button
              type="button"
              onClick={onClearWindow}
              className={segment({ active: true })}
              title={t("clearWindow")}
            >
              {t("window", {
                from: datetime(slot.from).slice(5, 16),
                to:
                  datetime(slot.to).slice(0, 10) === datetime(slot.from).slice(0, 10)
                    ? datetime(slot.to).slice(11, 16)
                    : datetime(slot.to).slice(5, 16),
              })}{" "}
              [x]
            </button>
          )}
          <button
            type="button"
            onClick={() => setFailed((f) => !f)}
            aria-pressed={failed}
            className={segment({ active: failed })}
          >
            {t("failedOnly")}
          </button>
        </span>
      }
    >
      <div className="max-h-96 overflow-y-auto border bg-muted/30 p-3 text-xs leading-6">
        {rows.map((r) => (
          <div key={r.id} className="flex gap-3 whitespace-nowrap">
            <span className="text-muted-foreground">{datetime(r.ts)}</span>
            <span className="w-24 shrink-0 truncate">{locations[r.location] ?? r.location}</span>
            <span className="w-8 shrink-0">
              <OkText ok={r.ok} />
            </span>
            <span className="w-16 shrink-0 text-right" title={phaseTitle(r.timing, r.latency_ms, tp)}>
              {ms(r.latency_ms)}
            </span>
            <span className="truncate text-muted-foreground" title={r.message}>
              {r.maint && <span className="text-info">[{tm("maint")}] </span>}
              {r.message}
            </span>
          </div>
        ))}
        {first.data && rows.length === 0 && (
          <span className="text-muted-foreground">
            {failed || slot ? t("none") : waitingText}
          </span>
        )}
        {rows.length > 0 && !done && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="mt-1 px-1"
            disabled={loading}
            onClick={loadMore}
          >
            {loading ? t("loading") : t("loadMore")}
          </Button>
        )}
      </div>
      {slot && <p className="mt-1 text-[11px] text-muted-foreground">{t("retentionHint")}</p>}
    </Section>
  )
}
