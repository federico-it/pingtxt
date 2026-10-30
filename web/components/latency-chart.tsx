"use client"

import { useEffect, useMemo, useRef, useState } from "react"

import type { Series } from "@/lib/api"
import { datetime, ms } from "@/lib/format"

const COLORS = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"]
const H = 180
const PAD = { l: 44, r: 8, t: 8, b: 20 }

function niceMax(v: number) {
  if (v <= 0) return 10
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p
  return 10 * p
}

export function LatencyChart({ series }: { series: Series }) {
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(600)
  const [hover, setHover] = useState<number | null>(null)

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

  function path(vals: (number | null)[]) {
    let d = ""
    let pen = false
    vals.forEach((v, i) => {
      if (v === null) {
        pen = false
        return
      }
      d += `${pen ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`
      pen = true
    })
    return d
  }

  function onMove(e: React.MouseEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const px = e.clientX - rect.left - PAD.l
    const i = Math.round((px / iw) * (slots - 1))
    setHover(i >= 0 && i < slots ? i : null)
  }

  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * yMax)

  if (series.locations.length === 0) {
    return <div className="flex h-[180px] items-center justify-center border border-dashed text-muted-foreground">no data yet</div>
  }

  return (
    <div ref={ref} className="relative w-full select-none">
      <svg width={w} height={H} onMouseMove={onMove} onMouseLeave={() => setHover(null)} className="block">
        {ticks.map((t) => (
          <g key={t}>
            <line x1={PAD.l} x2={w - PAD.r} y1={y(t)} y2={y(t)} stroke="var(--border)" strokeDasharray="2 3" />
            <text x={PAD.l - 6} y={y(t) + 3} textAnchor="end" fontSize={10} fill="var(--muted-foreground)">
              {ms(t) === "--" ? "0" : ms(t)}
            </text>
          </g>
        ))}
        {failSlots.map((f, i) =>
          f > 0 ? <rect key={i} x={x(i) - 1.5} y={PAD.t + ih + 3} width={3} height={5} fill="var(--down)" /> : null
        )}
        {series.locations.map((l, li) =>
          byLoc[l.id].map((v, i, arr) =>
            v !== null && arr[i - 1] == null && arr[i + 1] == null ? (
              <circle key={`${l.id}-${i}`} cx={x(i)} cy={y(v)} r={2} fill={COLORS[li % COLORS.length]} />
            ) : null
          )
        )}
        {series.locations.map((l, li) => (
          <path
            key={l.id}
            d={path(byLoc[l.id])}
            fill="none"
            stroke={COLORS[li % COLORS.length]}
            strokeWidth={1.5}
            strokeLinejoin="round"
          />
        ))}
        {hover !== null && (
          <line x1={x(hover)} x2={x(hover)} y1={PAD.t} y2={PAD.t + ih} stroke="var(--muted-foreground)" strokeDasharray="2 2" />
        )}
        <text x={PAD.l} y={H - 2} fontSize={10} fill="var(--muted-foreground)">
          {datetime(series.from)}
        </text>
        <text x={w - PAD.r} y={H - 2} fontSize={10} textAnchor="end" fill="var(--muted-foreground)">
          now
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
          {failSlots[hover] > 0 && <div className="text-down">{failSlots[hover]} failed checks</div>}
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
