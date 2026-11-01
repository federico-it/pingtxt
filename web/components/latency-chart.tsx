"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslations } from "use-intl"

import type { Series } from "@/lib/api"
import { datetime, ms } from "@/lib/format"
import { cn } from "@/lib/utils"

const COLORS = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"]
const H = 180
const PAD = { l: 44, r: 8, t: 8, b: 20 }

function niceMax(v: number) {
  if (v <= 0) return 10
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p
  return 10 * p
}

/** Shortest window the chart zooms into (the API allows 5 minutes). */
const MIN_ZOOM = 5 * 60_000

export function LatencyChart({
  series,
  live = true,
  onZoom,
}: {
  series: Series
  /** the window ends now (false: a past window, labelled with its end time) */
  live?: boolean
  /** called with a window [start, end) picked by dragging across the chart */
  onZoom?: (start: number, end: number) => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(600)
  const [hover, setHover] = useState<number | null>(null)
  // Slots where a drag started and where the pointer is now.
  const [drag, setDrag] = useState<[number, number] | null>(null)
  const t = useTranslations("latencyChart")

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver(([e]) => setW(Math.max(200, e.contentRect.width)))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const { slots, byLoc, yMax, failSlots } = useMemo(() => {
    const n = Math.max(1, Math.ceil((series.to - series.from) / series.size))
    const byLoc: Record<string, (number | null)[]> = {}
    const fails = new Array<number>(n).fill(0)
    for (const l of series.locations) byLoc[l.id] = new Array(n).fill(null)
    let max = 0
    for (const b of series.buckets) {
      const i = Math.floor((b.t - series.from) / series.size)
      if (i < 0 || i >= n) continue
      if (b.ok > 0) {
        byLoc[b.location][i] = b.avg_ms
        max = Math.max(max, b.avg_ms)
      }
      if (b.ok < b.total) fails[i] += b.total - b.ok
    }
    return { slots: n, byLoc, yMax: niceMax(max * 1.1), failSlots: fails }
  }, [series])

  const iw = w - PAD.l - PAD.r
  const ih = H - PAD.t - PAD.b
  const x = (i: number) => PAD.l + (slots <= 1 ? 0 : (i / (slots - 1)) * iw)
  const y = (v: number) => PAD.t + ih - (v / yMax) * ih

  // Lines join consecutive points even across empty slots: older data comes
  // from hourly rollups, so points are naturally further apart than the slot
  // size. Only a gap much longer than the series' usual spacing (agent offline,
  // monitor paused) breaks the line.
  function segments(vals: (number | null)[]) {
    const idx: number[] = []
    vals.forEach((v, i) => v !== null && idx.push(i))
    const gaps = idx.slice(1).map((v, k) => v - idx[k]).sort((a, b) => a - b)
    const typical = gaps.length ? gaps[Math.floor(gaps.length / 2)] : 1
    const maxGap = Math.max(3, typical * 4)
    const segs: number[][] = []
    idx.forEach((i, k) => {
      if (k === 0 || i - idx[k - 1] > maxGap) segs.push([i])
      else segs[segs.length - 1].push(i)
    })
    return segs
  }

  function path(vals: (number | null)[], segs: number[][]) {
    return segs
      .filter((s) => s.length > 1)
      .map((s) => s.map((i, k) => `${k ? "L" : "M"}${x(i).toFixed(1)},${y(vals[i]!).toFixed(1)}`).join(""))
      .join("")
  }

  function slotAt(e: React.PointerEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const px = e.clientX - rect.left - PAD.l
    return Math.round((px / iw) * (slots - 1))
  }

  function onMove(e: React.PointerEvent<SVGSVGElement>) {
    const i = slotAt(e)
    setHover(i >= 0 && i < slots ? i : null)
    if (drag) setDrag([drag[0], Math.min(Math.max(i, 0), slots - 1)])
  }

  // Drag with a mouse or pen to zoom; touch keeps scrolling the page.
  function onDown(e: React.PointerEvent<SVGSVGElement>) {
    if (!onZoom || e.pointerType === "touch" || e.button !== 0) return
    const i = slotAt(e)
    if (i < 0 || i >= slots) return
    e.currentTarget.setPointerCapture(e.pointerId)
    setDrag([i, i])
  }

  function onUp() {
    if (!drag || !onZoom) return
    setDrag(null)
    const [a, b] = [Math.min(...drag), Math.max(...drag)]
    if (b - a < 1) return // a click, not a drag
    let start = series.from + a * series.size
    let end = Math.min(series.from + (b + 1) * series.size, series.to)
    if (end - start < MIN_ZOOM) {
      const mid = (start + end) / 2
      start = Math.round(mid - MIN_ZOOM / 2)
      end = start + MIN_ZOOM
    }
    onZoom(start, end)
  }

  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * yMax)

  if (series.locations.length === 0) {
    return <div className="flex h-[180px] items-center justify-center border border-dashed text-muted-foreground">{t("noData")}</div>
  }

  return (
    <div ref={ref} className="relative w-full select-none">
      <svg
        width={w}
        height={H}
        onPointerMove={onMove}
        onPointerDown={onDown}
        onPointerUp={onUp}
        onPointerCancel={() => setDrag(null)}
        onPointerLeave={() => setHover(null)}
        className={cn("block", onZoom && "cursor-crosshair")}
      >
        {ticks.map((v) => (
          <g key={v}>
            <line x1={PAD.l} x2={w - PAD.r} y1={y(v)} y2={y(v)} stroke="var(--border)" strokeDasharray="2 3" />
            <text x={PAD.l - 6} y={y(v) + 3} textAnchor="end" fontSize={10} fill="var(--muted-foreground)">
              {ms(v) === "--" ? "0" : ms(v)}
            </text>
          </g>
        ))}
        {failSlots.map((f, i) =>
          f > 0 ? <rect key={i} x={x(i) - 1.5} y={PAD.t + ih + 3} width={3} height={5} fill="var(--down)" /> : null
        )}
        {series.locations.map((l, li) => {
          const vals = byLoc[l.id]
          const segs = segments(vals)
          const color = COLORS[li % COLORS.length]
          return (
            <g key={l.id}>
              <path d={path(vals, segs)} fill="none" stroke={color} strokeWidth={1.5} strokeLinejoin="round" />
              {segs
                .filter((s) => s.length === 1)
                .map(([i]) => (
                  <circle key={i} cx={x(i)} cy={y(vals[i]!)} r={2} fill={color} />
                ))}
            </g>
          )
        })}
        {drag && drag[0] !== drag[1] && (
          <rect
            x={x(Math.min(...drag))}
            y={PAD.t}
            width={Math.abs(x(drag[1]) - x(drag[0]))}
            height={ih}
            fill="var(--primary)"
            fillOpacity={0.12}
            stroke="var(--primary)"
            strokeOpacity={0.5}
          />
        )}
        {hover !== null && (
          <line x1={x(hover)} x2={x(hover)} y1={PAD.t} y2={PAD.t + ih} stroke="var(--muted-foreground)" strokeDasharray="2 2" />
        )}
        <text x={PAD.l} y={H - 2} fontSize={10} fill="var(--muted-foreground)">
          {datetime(series.from)}
        </text>
        <text x={w - PAD.r} y={H - 2} fontSize={10} textAnchor="end" fill="var(--muted-foreground)">
          {live ? t("now") : datetime(series.to)}
        </text>
      </svg>
      {hover !== null && (
        <div
          className="pointer-events-none absolute top-2 z-10 border bg-popover px-2 py-1 text-xs shadow-sm"
          style={{ left: Math.min(x(hover) + 8, w - 190) }}
        >
          <div className="text-muted-foreground">{datetime(series.from + hover * series.size)}</div>
          {series.locations.map((l, li) => (
            <div key={l.id} className="flex justify-between gap-4">
              <span style={{ color: COLORS[li % COLORS.length] }}>■ {l.name}</span>
              <span>{ms(byLoc[l.id][hover] ?? 0)}</span>
            </div>
          ))}
          {failSlots[hover] > 0 && <div className="text-down">{t("failed", { count: failSlots[hover] })}</div>}
        </div>
      )}
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {series.locations.map((l, li) => (
          <span key={l.id}>
            <span style={{ color: COLORS[li % COLORS.length] }}>■</span> {l.name}
          </span>
        ))}
      </div>
    </div>
  )
}
