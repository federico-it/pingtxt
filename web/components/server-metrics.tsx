"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslations } from "use-intl"

import { CountryFlag } from "@/components/country"
import { Section } from "@/components/page-header"
import { segment } from "@/components/ui/control"
import {
  type ServerHistory,
  type ServerLimits,
  type ServerMetrics,
  type ServerSample,
  useApi,
} from "@/lib/api"
import { ago, datetime, duration } from "@/lib/format"
import { cn } from "@/lib/utils"

const ranges = { "24h": 24, "7d": 168 } as const
type Range = keyof typeof ranges

/** Bytes as KB, MB, GB... with one decimal. */
export function bytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB", "PB"]
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function pctOf(a: number, b: number) {
  return b > 0 ? (a / b) * 100 : 0
}

/** A usage bar with its value, red above the limit. */
function Usage({
  label,
  value,
  limit,
  detail,
}: {
  label: string
  value: number
  limit?: number
  detail?: string
}) {
  const over = !!limit && value > limit
  return (
    <div className="grid gap-1">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="truncate text-muted-foreground">{label}</span>
        <span className={cn("shrink-0 tabular-nums", over && "text-down")}>
          {value.toFixed(0)}%
          {detail && (
            <span className="ml-1 text-muted-foreground">{detail}</span>
          )}
        </span>
      </div>
      <div className="relative h-1.5 bg-muted">
        <div
          className={cn("h-full", over ? "bg-down" : "bg-up")}
          style={{ width: `${Math.min(Math.max(value, 0), 100)}%` }}
        />
        {!!limit && (
          <div
            className="absolute top-[-2px] h-[10px] w-px bg-foreground/60"
            style={{ left: `${limit}%` }}
          />
        )}
      </div>
    </div>
  )
}

function Snapshot({
  m,
  limits,
}: {
  m: ServerMetrics
  limits: ServerLimits
}) {
  const t = useTranslations("serverMetrics")
  const perCore = m.load5 / Math.max(m.cores, 1)
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Usage label={t("cpu")} value={m.cpu_pct} limit={limits.cpu_pct} />
      <Usage
        label={t("ram")}
        value={pctOf(m.mem_used, m.mem_total)}
        limit={limits.mem_pct}
        detail={`${bytes(m.mem_used)} / ${bytes(m.mem_total)}`}
      />
      {m.swap_total > 0 && (
        <Usage
          label={t("swap")}
          value={pctOf(m.swap_used, m.swap_total)}
          limit={limits.swap_pct}
          detail={`${bytes(m.swap_used)} / ${bytes(m.swap_total)}`}
        />
      )}
      {(m.disks ?? []).map((d) => (
        <Usage
          key={d.mount}
          label={`${t("disk")} ${d.mount}`}
          value={pctOf(d.used, d.used + d.free)}
          limit={limits.disk_pct}
          detail={`${bytes(d.used)} / ${bytes(d.total)}`}
        />
      ))}
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground sm:col-span-2">
        <span className={cn(!!limits.load && perCore > limits.load && "text-down")}>
          {t("load", {
            l1: m.load1.toFixed(2),
            l5: m.load5.toFixed(2),
            l15: m.load15.toFixed(2),
            cores: m.cores,
          })}
        </span>
        {m.uptime_sec > 0 && (
          <span>{t("uptime", { time: duration(m.uptime_sec * 1000) })}</span>
        )}
      </div>
    </div>
  )
}

const LINES = [
  { key: "cpu_pct", color: "var(--chart-1)" },
  { key: "mem_pct", color: "var(--chart-2)" },
  { key: "disk_pct", color: "var(--chart-3)" },
] as const
const H = 160
const PAD = { l: 36, r: 8, t: 8, b: 20 }

function Chart({
  points,
  from,
  to,
}: {
  points: ServerSample[]
  from: number
  to: number
}) {
  const t = useTranslations("serverMetrics")
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(600)
  const [hover, setHover] = useState<number | null>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver(([e]) =>
      setW(Math.max(200, e.contentRect.width))
    )
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  const iw = w - PAD.l - PAD.r
  const ih = H - PAD.t - PAD.b
  const x = (ts: number) => PAD.l + ((ts - from) / Math.max(to - from, 1)) * iw
  const y = (v: number) => PAD.t + ih - (Math.min(Math.max(v, 0), 100) / 100) * ih
  // Lines break where checks are missing for more than 3 buckets.
  const gap = ((to - from) / 300) * 3 + 120_000
  const paths = LINES.map((l) => {
    let d = ""
    let prev = 0
    for (const p of points) {
      const cmd = d === "" || p.ts - prev > gap ? "M" : "L"
      d += `${cmd}${x(p.ts).toFixed(1)},${y(p[l.key]).toFixed(1)}`
      prev = p.ts
    }
    return { ...l, d }
  })
  const hp = hover !== null ? points[hover] : null
  return (
    <div ref={ref} className="relative">
      <div className="mb-1 flex flex-wrap gap-3 text-xs text-muted-foreground">
        {LINES.map((l) => (
          <span key={l.key} className="flex items-center gap-1">
            <span className="inline-block h-0.5 w-3" style={{ background: l.color }} />
            {t(`lines.${l.key}`)}
          </span>
        ))}
      </div>
      <svg
        width={w}
        height={H}
        className="block touch-none"
        onPointerMove={(e) => {
          if (points.length === 0) return
          const r = e.currentTarget.getBoundingClientRect()
          const ts = from + ((e.clientX - r.left - PAD.l) / iw) * (to - from)
          let best = 0
          for (let i = 1; i < points.length; i++)
            if (Math.abs(points[i].ts - ts) < Math.abs(points[best].ts - ts)) best = i
          setHover(best)
        }}
        onPointerLeave={() => setHover(null)}
      >
        {[0, 50, 100].map((v) => (
          <g key={v}>
            <line
              x1={PAD.l}
              x2={w - PAD.r}
              y1={y(v)}
              y2={y(v)}
              className="stroke-border"
              strokeDasharray="2 3"
            />
            <text
              x={PAD.l - 6}
              y={y(v) + 3}
              textAnchor="end"
              className="fill-muted-foreground text-[10px]"
            >
              {v}%
            </text>
          </g>
        ))}
        {paths.map((p) => (
          <path key={p.key} d={p.d} fill="none" stroke={p.color} strokeWidth={1.5} />
        ))}
        {points.length < 60 &&
          LINES.map((l) =>
            points.map((p) => (
              <circle
                key={`${l.key}-${p.ts}`}
                cx={x(p.ts)}
                cy={y(p[l.key])}
                r={1.5}
                fill={l.color}
              />
            ))
          )}
        {hp && (
          <line
            x1={x(hp.ts)}
            x2={x(hp.ts)}
            y1={PAD.t}
            y2={PAD.t + ih}
            className="stroke-foreground/40"
          />
        )}
      </svg>
      {hp && (
        <div
          className="pointer-events-none absolute top-6 border bg-popover px-2 py-1 text-xs shadow-sm"
          style={{
            left: Math.min(x(hp.ts) + 8, w - 150),
          }}
        >
          <div className="text-muted-foreground">{datetime(hp.ts)}</div>
          {LINES.map((l) => (
            <div key={l.key} className="tabular-nums">
              {t(`lines.${l.key}`)}: {hp[l.key].toFixed(1)}%
            </div>
          ))}
          <div className="tabular-nums">
            {t("load5")}: {hp.load5.toFixed(2)}
          </div>
        </div>
      )}
    </div>
  )
}

/** Metrics of a server monitor: the latest snapshot of each location and a chart. */
export function ServerPanel({
  id,
  limits,
  countries,
}: {
  id: number
  limits: ServerLimits
  countries?: Record<string, string | undefined>
}) {
  const t = useTranslations("serverMetrics")
  const [range, setRange] = useState<Range>("24h")
  const [loc, setLoc] = useState<string | null>(null)
  const res = useApi<ServerHistory>(
    `/api/monitors/${id}/server?hours=${ranges[range]}`,
    { refreshInterval: 30_000 }
  )
  const data = res.data
  const locs = data?.locations ?? []
  const current = loc ?? locs[0]?.location ?? null
  const points = useMemo(
    () => (data?.points ?? []).filter((p) => p.location === current),
    [data, current]
  )
  return (
    <>
      <Section title={t("title")}>
        {locs.length === 0 && (
          <p className="text-xs text-muted-foreground">{t("loading")}</p>
        )}
        <div className="grid gap-6">
          {locs.map((l) => (
            <div key={l.location} className="grid gap-2">
              <div className="flex items-baseline justify-between gap-2 text-sm">
                <span className="truncate">
                  <CountryFlag code={countries?.[l.location]} />
                  {l.name}
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {l.at ? ago(l.at) : t("noData")}
                </span>
              </div>
              {l.metrics ? (
                <Snapshot m={l.metrics} limits={limits} />
              ) : (
                <p className="text-xs text-muted-foreground">{t("noDataHint")}</p>
              )}
            </div>
          ))}
        </div>
      </Section>
      <Section
        title={t("history")}
        actions={
          <div className="flex gap-1 normal-case">
            {(Object.keys(ranges) as Range[]).map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={segment({ active: range === r })}
              >
                {r}
              </button>
            ))}
          </div>
        }
      >
        {locs.length > 1 && (
          <div className="mb-2 flex flex-wrap gap-1 text-xs">
            {locs.map((l) => (
              <button
                key={l.location}
                onClick={() => setLoc(l.location)}
                className={segment({ active: current === l.location })}
              >
                {l.name}
              </button>
            ))}
          </div>
        )}
        {data && points.length > 0 ? (
          <Chart points={points} from={data.from} to={data.to} />
        ) : (
          <p className="text-xs text-muted-foreground">{t("noHistory")}</p>
        )}
      </Section>
    </>
  )
}
