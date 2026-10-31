"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslations } from "use-intl"

import type { Phases, Series } from "@/lib/api"
import { datetime, ms } from "@/lib/format"

const H = 180
const PAD = { l: 44, r: 8, t: 8, b: 20 }

// Stacked bottom to top. The order keeps neighbouring colors apart for color
// blind readers too; the rest (reading the body) is neutral.
export const PHASES = [
  { key: "dns", color: "var(--chart-5)" },
  { key: "connect", color: "var(--chart-3)" },
  { key: "tls", color: "var(--chart-2)" },
  { key: "wait", color: "var(--chart-1)" },
  { key: "transfer", color: "color-mix(in oklab, var(--muted-foreground) 45%, transparent)" },
] as const

type Key = (typeof PHASES)[number]["key"]
type Vals = Record<Key, number>

/** Splits a check (or an average) into the stacked values; the rest of the
 * total is reading the body. */
export function phaseValues(p: Phases, total: number): Vals {
  const sum = p.dns_ms + p.connect_ms + p.tls_ms + p.ttfb_ms
  return {
    dns: p.dns_ms,
    connect: p.connect_ms,
    tls: p.tls_ms,
    wait: p.ttfb_ms,
    transfer: Math.max(0, total - sum),
  }
}

function niceMax(v: number) {
  if (v <= 0) return 10
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p
  return 10 * p
}

// Weighted average of the phases of several buckets (locations or slots).
function merge(list: Phases[]): Phases | null {
  let n = 0
  const s = { dns_ms: 0, connect_ms: 0, tls_ms: 0, ttfb_ms: 0, total_ms: 0 }
  for (const p of list) {
    const w = p.n ?? 1
    n += w
    s.dns_ms += p.dns_ms * w
    s.connect_ms += p.connect_ms * w
    s.tls_ms += p.tls_ms * w
    s.ttfb_ms += p.ttfb_ms * w
    s.total_ms += (p.total_ms ?? 0) * w
  }
  if (n === 0) return null
  return {
    dns_ms: s.dns_ms / n,
    connect_ms: s.connect_ms / n,
    tls_ms: s.tls_ms / n,
    ttfb_ms: s.ttfb_ms / n,
    total_ms: s.total_ms / n,
    n,
  }
}

/** True when the series has any check that measured phases. */
export function hasPhases(series: Series | undefined) {
  return !!series?.buckets.some((b) => b.phases)
}

/** Locations with phase data, to pick one for the chart. */
export function phaseLocations(series: Series) {
  return series.locations.filter((l) => series.buckets.some((b) => b.location === l.id && b.phases))
}

/** Response time split by phase (DNS, connect, TLS, wait, transfer), as
 * stacked columns over time, for one location ("" = all together). */
export function PhaseChart({ series, location = "" }: { series: Series; location?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(600)
  const [hover, setHover] = useState<number | null>(null)
  const t = useTranslations("phaseChart")

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver(([e]) => setW(Math.max(200, e.contentRect.width)))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const sel = location

  const { slots, vals, yMax, avg } = useMemo(() => {
    const n = Math.max(1, Math.ceil((series.to - series.from) / series.size))
    const per: Phases[][] = Array.from({ length: n }, () => [])
    const all: Phases[] = []
    for (const b of series.buckets) {
      if (!b.phases || (sel && b.location !== sel)) continue
      const i = Math.floor((b.t - series.from) / series.size)
      if (i < 0 || i >= n) continue
      per[i].push(b.phases)
      all.push(b.phases)
    }
    let max = 0
    const vals = per.map((list) => {
      const p = merge(list)
      if (!p) return null
      const v = phaseValues(p, p.total_ms ?? 0)
      max = Math.max(max, Object.values(v).reduce((a, x) => a + x, 0))
      return v
    })
    const a = merge(all)
    return { slots: n, vals, yMax: niceMax(max * 1.1), avg: a ? phaseValues(a, a.total_ms ?? 0) : null }
  }, [series, sel])

  const iw = w - PAD.l - PAD.r
  const ih = H - PAD.t - PAD.b
  const slotW = iw / slots
  const barW = Math.max(1, slotW - (slotW > 4 ? 1.5 : 0.5))
  const x = (i: number) => PAD.l + i * slotW
  const y = (v: number) => PAD.t + ih - (v / yMax) * ih
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * yMax)

  function onMove(e: React.MouseEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const i = Math.floor((e.clientX - rect.left - PAD.l) / slotW)
    setHover(i >= 0 && i < slots ? i : null)
  }

  if (!avg) {
    return <div className="flex h-[180px] items-center justify-center border border-dashed text-muted-foreground">{t("noData")}</div>
  }
  const hv = hover !== null ? vals[hover] : null

  return (
    <div ref={ref} className="relative w-full select-none">
      <svg width={w} height={H} onMouseMove={onMove} onMouseLeave={() => setHover(null)} className="block">
        {ticks.map((v) => (
          <g key={v}>
            <line x1={PAD.l} x2={w - PAD.r} y1={y(v)} y2={y(v)} stroke="var(--border)" strokeDasharray="2 3" />
            <text x={PAD.l - 6} y={y(v) + 3} textAnchor="end" fontSize={10} fill="var(--muted-foreground)">
              {ms(v) === "--" ? "0" : ms(v)}
            </text>
          </g>
        ))}
        {vals.map((v, i) => {
          if (!v) return null
          let acc = 0
          return (
            <g key={i} opacity={hover === null || hover === i ? 1 : 0.55}>
              {PHASES.map((p) => {
                const h = (v[p.key] / yMax) * ih
                acc += v[p.key]
                if (h <= 0) return null
                return <rect key={p.key} x={x(i)} y={y(acc)} width={barW} height={h} fill={p.color} />
              })}
            </g>
          )
        })}
        <text x={PAD.l} y={H - 2} fontSize={10} fill="var(--muted-foreground)">
          {datetime(series.from)}
        </text>
        <text x={w - PAD.r} y={H - 2} fontSize={10} textAnchor="end" fill="var(--muted-foreground)">
          {t("now")}
        </text>
      </svg>
      {hover !== null && hv && (
        <div
          className="pointer-events-none absolute top-2 z-10 border bg-popover px-2 py-1 text-xs shadow-sm"
          style={{ left: Math.min(x(hover) + slotW + 8, w - 190) }}
        >
          <div className="text-muted-foreground">{datetime(series.from + hover * series.size)}</div>
          {[...PHASES].reverse().map((p) => (
            <div key={p.key} className="flex justify-between gap-4">
              <span>
                <span style={{ color: p.color }}>■</span> {t(p.key)}
              </span>
              <span>{ms(hv[p.key])}</span>
            </div>
          ))}
          <div className="mt-0.5 flex justify-between gap-4 border-t pt-0.5">
            <span>{t("total")}</span>
            <span>{ms(Object.values(hv).reduce((a, x) => a + x, 0))}</span>
          </div>
        </div>
      )}
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {PHASES.map((p) => (
          <span key={p.key}>
            <span style={{ color: p.color }}>■</span> {t(p.key)} <span className="text-foreground">{ms(avg[p.key])}</span>
          </span>
        ))}
        <span className="ml-auto">{t("average")}</span>
      </div>
    </div>
  )
}

/** One line breakdown of a single check, for titles. */
export function phaseTitle(p: Phases | undefined, total: number, t: (k: Key) => string) {
  if (!p) return undefined
  const v = phaseValues(p, total)
  return PHASES.map((x) => `${t(x.key)} ${ms(v[x.key])}`).join(" · ")
}
