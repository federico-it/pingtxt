"use client"

import { useTranslations } from "use-intl"

import type { PeriodStats } from "@/lib/api"
import { datetime, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

/**
 * Uptime, average and latency percentiles of the chart window, each compared
 * with the window of the same length just before it.
 */
export function PeriodStatsRow({
  current,
  previous,
}: {
  current: PeriodStats
  previous: PeriodStats
}) {
  const t = useTranslations("periodStats")
  const cp = current.percentiles
  const pp = previous.percentiles
  const hasPct = cp.n > 0
  const prevPct = pp.n > 0
  // Older checks are kept only as hourly averages: percentiles then cover
  // only the end of the window.
  const partial = hasPct && cp.from > current.from

  const items: { k: string; v: string; delta?: React.ReactNode; title?: string }[] = [
    {
      k: t("uptime"),
      v: pct(current.uptime),
      delta: current.uptime >= 0 && previous.uptime >= 0 ? <UptimeDelta cur={current.uptime} prev={previous.uptime} /> : null,
    },
    {
      k: t("avg"),
      v: ms(current.avg_ms),
      delta: current.avg_ms > 0 && previous.avg_ms > 0 ? <LatencyDelta cur={current.avg_ms} prev={previous.avg_ms} /> : null,
    },
    ...(["p50", "p95", "p99"] as const).map((p) => ({
      k: p,
      v: hasPct ? ms(cp[p]) : "--",
      delta: hasPct && prevPct ? <LatencyDelta cur={cp[p]} prev={pp[p]} /> : null,
      title: t(`${p}Hint`),
    })),
  ]

  return (
    <div className="mt-3 border-t border-dashed pt-3">
      <div className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-5">
        {items.map((i) => (
          <div key={i.k} className="min-w-0" title={i.title}>
            <div className="text-[11px] uppercase text-muted-foreground">{i.k}</div>
            <div className="flex flex-wrap items-baseline gap-x-2">
              <span className="text-sm">{i.v}</span>
              {i.delta}
            </div>
          </div>
        ))}
      </div>
      <div className="mt-2 text-[11px] text-muted-foreground">
        {t("compared", { from: datetime(previous.from), to: datetime(previous.to) })}
        {partial && <> · {t("partial", { from: datetime(cp.from) })}</>}
        {hasPct && <> · {t("checks", { count: cp.n })}</>}
      </div>
    </div>
  )
}

/** Change in latency: lower is better. Changes under 3% read as flat. */
function LatencyDelta({ cur, prev }: { cur: number; prev: number }) {
  const t = useTranslations("periodStats")
  const d = (cur - prev) / prev
  if (Math.abs(d) < 0.03) return <span className="text-xs text-muted-foreground">{t("flat")}</span>
  const diff = cur - prev
  return (
    <span
      className={cn("text-xs", d > 0 ? (d > 0.25 ? "text-down" : "text-warn") : "text-up")}
      title={t("was", { v: ms(prev) })}
    >
      {d > 0 ? "▲" : "▼"} {d > 0 ? "+" : "-"}
      {ms(Math.abs(diff)) === "--" ? "0ms" : ms(Math.abs(diff))} ({d > 0 ? "+" : ""}
      {Math.round(d * 100)}%)
    </span>
  )
}

/** Change in uptime, in percentage points: higher is better. */
function UptimeDelta({ cur, prev }: { cur: number; prev: number }) {
  const t = useTranslations("periodStats")
  const d = (cur - prev) * 100
  if (Math.abs(d) < 0.01) return <span className="text-xs text-muted-foreground">{t("flat")}</span>
  return (
    <span className={cn("text-xs", d > 0 ? "text-up" : "text-down")} title={t("was", { v: pct(prev) })}>
      {d > 0 ? "▲ +" : "▼ "}
      {d.toFixed(2)} {t("points")}
    </span>
  )
}
