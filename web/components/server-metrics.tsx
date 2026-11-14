"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslations } from "use-intl"

import { CountryFlag } from "@/components/country"
import { KV, Section } from "@/components/page-header"
import { StatusText } from "@/components/status"
import { segment } from "@/components/ui/control"
import {
  type MonitorDetail,
  type ServerHistory,
  type ServerLimits,
  type ServerMetrics,
  type ServerSample,
  useApi,
} from "@/lib/api"
import { ago, datetime, duration } from "@/lib/format"
import { cn } from "@/lib/utils"

const ranges = { "1h": 1, "6h": 6, "24h": 24, "7d": 168 } as const
type Range = keyof typeof ranges

const noLimits: ServerLimits = {
  cpu_pct: 0,
  mem_pct: 0,
  disk_pct: 0,
  swap_pct: 0,
  load: 0,
}

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

function fullestDisk(m: ServerMetrics) {
  let best: { mount: string; pct: number } | null = null
  for (const d of m.disks ?? []) {
    const p = pctOf(d.used, d.used + d.free)
    if (!best || p > best.pct) best = { mount: d.mount, pct: p }
  }
  return best
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

/** Memory, swap and every disk of the latest check, as bars. */
function Snapshot({ m, limits }: { m: ServerMetrics; limits: ServerLimits }) {
  const t = useTranslations("serverMetrics")
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
      <div className="text-xs text-muted-foreground sm:col-span-2">
        {t("load", {
          l1: m.load1.toFixed(2),
          l5: m.load5.toFixed(2),
          l15: m.load15.toFixed(2),
          cores: m.cores,
        })}
      </div>
    </div>
  )
}

/** A big value with its limit under it, red when over. */
function Metric({
  value,
  limit,
  unit = "%",
  digits = 0,
}: {
  value: number
  limit?: number
  unit?: string
  digits?: number
}) {
  const t = useTranslations("serverMetrics")
  const over = !!limit && value > limit
  return (
    <span className="flex items-baseline gap-2">
      <span className={cn("tabular-nums", over && "text-down")}>
        {value.toFixed(digits)}
        {unit}
      </span>
      <span className="text-[11px] text-muted-foreground">
        {limit ? t("limitShort", { limit: `${limit}${unit}` }) : t("noLimit")}
      </span>
    </span>
  )
}

type Key = "cpu_pct" | "mem_pct" | "disk_pct" | "swap_pct" | "load"
const H = 120
const PAD = { l: 34, r: 8, t: 6, b: 18 }

function clockLabel(ts: number, long: boolean) {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, "0")
  return long
    ? `${p(d.getDate())}/${p(d.getMonth() + 1)}`
    : `${p(d.getHours())}:${p(d.getMinutes())}`
}

/** One metric over time, with its limit as a dashed red line. */
function MetricChart({
  title,
  color,
  points,
  value,
  from,
  to,
  gap,
  limit,
  max,
  format,
  hover,
  onHover,
}: {
  title: string
  color: string
  points: ServerSample[]
  value: (p: ServerSample) => number
  from: number
  to: number
  gap: number
  limit?: number
  /** top of the scale; percentages use 100 */
  max: number
  format: (v: number) => string
  hover: number | null
  onHover: (ts: number | null) => void
}) {
  const t = useTranslations("serverMetrics")
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(600)
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
  const y = (v: number) =>
    PAD.t + ih - (Math.min(Math.max(v, 0), max) / max) * ih
  // Runs of points without a gap: each becomes a line with a light area.
  const runs: ServerSample[][] = []
  let prev = 0
  for (const p of points) {
    if (runs.length === 0 || p.ts - prev > gap) runs.push([])
    runs[runs.length - 1].push(p)
    prev = p.ts
  }
  const line = (r: ServerSample[]) =>
    r
      .map(
        (p, i) =>
          `${i ? "L" : "M"}${x(p.ts).toFixed(1)},${y(value(p)).toFixed(1)}`
      )
      .join("")
  const area = (r: ServerSample[]) =>
    `${line(r)}L${x(r[r.length - 1].ts).toFixed(1)},${y(0)}L${x(r[0].ts).toFixed(1)},${y(0)}Z`
  let hp: ServerSample | null = null
  if (hover !== null && points.length > 0) {
    hp = points[0]
    for (const p of points)
      if (Math.abs(p.ts - hover) < Math.abs(hp.ts - hover)) hp = p
  }
  const last = points[points.length - 1]
  const shown = hp ?? last
  const over = !!limit && !!shown && value(shown) > limit
  const long = to - from > 36 * 3600_000
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => from + f * (to - from))
  return (
    <div ref={ref} className="min-w-0">
      <div className="mb-1 flex items-baseline justify-between gap-2 text-xs">
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <span
            className="inline-block h-0.5 w-3"
            style={{ background: color }}
          />
          {title}
        </span>
        <span className="tabular-nums">
          {shown && (
            <span className={cn(over && "text-down")}>
              {format(value(shown))}
            </span>
          )}
          {hp && (
            <span className="ml-2 text-muted-foreground">
              {datetime(hp.ts)}
            </span>
          )}
          {!hp && !!limit && (
            <span className="ml-2 text-muted-foreground">
              {t("limitShort", { limit: format(limit) })}
            </span>
          )}
        </span>
      </div>
      <svg
        width={w}
        height={H}
        className="block touch-none"
        onPointerMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect()
          onHover(from + ((e.clientX - r.left - PAD.l) / iw) * (to - from))
        }}
        onPointerLeave={() => onHover(null)}
      >
        {[0, max / 2, max].map((v) => (
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
              {format(v)}
            </text>
          </g>
        ))}
        {ticks.map((ts, i) => (
          <text
            key={ts}
            x={x(ts)}
            y={H - 4}
            textAnchor={
              i === 0 ? "start" : i === ticks.length - 1 ? "end" : "middle"
            }
            className="fill-muted-foreground text-[10px]"
          >
            {clockLabel(ts, long)}
          </text>
        ))}
        {runs.map((r) => (
          <g key={r[0].ts}>
            <path d={area(r)} fill={color} fillOpacity={0.1} />
            <path
              d={line(r)}
              fill="none"
              stroke={color}
              strokeWidth={1.5}
              strokeLinejoin="round"
            />
            {r.length === 1 && (
              <circle cx={x(r[0].ts)} cy={y(value(r[0]))} r={2} fill={color} />
            )}
          </g>
        ))}
        {!!limit && limit <= max && (
          <line
            x1={PAD.l}
            x2={w - PAD.r}
            y1={y(limit)}
            y2={y(limit)}
            className="stroke-down"
            strokeDasharray="4 3"
          />
        )}
        {hp && (
          <>
            <line
              x1={x(hp.ts)}
              x2={x(hp.ts)}
              y1={PAD.t}
              y2={PAD.t + ih}
              className="stroke-foreground/40"
            />
            <circle cx={x(hp.ts)} cy={y(value(hp))} r={2.5} fill={color} />
          </>
        )}
      </svg>
    </div>
  )
}

/** CPU, memory, disk, swap and load over time, one small chart each. */
function History({
  data,
  location,
  cores,
  limits,
  intervalSec,
  hasSwap,
}: {
  data: ServerHistory
  location: string
  cores: number
  limits: ServerLimits
  intervalSec: number
  hasSwap: boolean
}) {
  const t = useTranslations("serverMetrics")
  const [hover, setHover] = useState<number | null>(null)
  const points = useMemo(
    () => (data.points ?? []).filter((p) => p.location === location),
    [data, location]
  )
  if (points.length === 0)
    return <p className="text-xs text-muted-foreground">{t("noHistory")}</p>
  const bucket = (data.to - data.from) / 300
  const gap = Math.max(bucket, intervalSec * 1000) * 2.5
  // A monitor younger than the window: the charts start at its first
  // check instead of squeezing it into the right edge.
  const first = points[0].ts
  const from =
    first - data.from > (data.to - data.from) * 0.1
      ? first - Math.max(gap / 5, 60_000)
      : data.from
  const pctFmt = (v: number) => `${Math.round(v)}%`
  const perCore = (p: ServerSample) => p.load5 / Math.max(cores, 1)
  // Round the top of the load scale to half a unit: 0, 1.00, 2.00.
  const loadMax =
    Math.ceil(
      Math.max(1, limits.load * 1.25, ...points.map(perCore)) * 1.05 * 2
    ) / 2
  const common = { points, from, to: data.to, gap, hover, onHover: setHover }
  const charts: {
    key: Key
    title: string
    color: string
    limit: number
    value: (p: ServerSample) => number
    max: number
    format: (v: number) => string
  }[] = [
    {
      key: "cpu_pct",
      title: t("cpu"),
      color: "var(--chart-1)",
      limit: limits.cpu_pct,
      value: (p) => p.cpu_pct,
      max: 100,
      format: pctFmt,
    },
    {
      key: "mem_pct",
      title: t("ram"),
      color: "var(--chart-2)",
      limit: limits.mem_pct,
      value: (p) => p.mem_pct,
      max: 100,
      format: pctFmt,
    },
    {
      key: "disk_pct",
      title: t("fullestDisk"),
      color: "var(--chart-3)",
      limit: limits.disk_pct,
      value: (p) => p.disk_pct,
      max: 100,
      format: pctFmt,
    },
    ...(hasSwap
      ? [
          {
            key: "swap_pct" as Key,
            title: t("swap"),
            color: "var(--chart-4)",
            limit: limits.swap_pct,
            value: (p: ServerSample) => p.swap_pct,
            max: 100,
            format: pctFmt,
          },
        ]
      : []),
    {
      key: "load",
      title: t("loadPerCore"),
      color: "var(--chart-5)",
      limit: limits.load,
      value: perCore,
      max: loadMax,
      format: (v: number) => v.toFixed(2),
    },
  ]
  return (
    <>
      {from > data.from && (
        <p className="mb-3 text-xs text-muted-foreground">
          {t("since", { time: datetime(first) })}
        </p>
      )}
      <div className="grid gap-x-8 gap-y-5 md:grid-cols-2">
        {charts.map(({ key, ...c }) => (
          <MetricChart key={key} {...common} {...c} />
        ))}
      </div>
    </>
  )
}

/**
 * The page of a server monitor from the status row to the history: the
 * latest values first, then the machine's bars and one chart per metric.
 * Latency, response time and the location table mean nothing here.
 */
export function ServerView({ d, now }: { d: MonitorDetail; now: number }) {
  const t = useTranslations("serverMetrics")
  const tm = useTranslations("monitor")
  const limits = d.server_limits ?? noLimits
  // A young monitor opens on the shortest window that holds all its
  // data, so its first checks draw lines instead of a dot or two.
  const age = now - d.created_at
  const [range, setRange] = useState<Range>(
    age < 3600_000 ? "1h" : age < 6 * 3600_000 ? "6h" : "24h"
  )
  const [loc, setLoc] = useState<string | null>(null)
  const res = useApi<ServerHistory>(
    `/api/monitors/${d.id}/server?hours=${ranges[range]}`,
    {
      refreshInterval: 30_000,
      keepPreviousData: true,
    }
  )
  const data = res.data
  const locs = data?.locations ?? []
  const current = locs.find((l) => l.location === loc) ?? locs[0]
  const m = current?.metrics ?? null
  const status = new Map(d.location_status.map((l) => [l.id, l]))
  const disk = m ? fullestDisk(m) : null
  const perCore = m ? m.load5 / Math.max(m.cores, 1) : 0
  return (
    <>
      <div className="grid grid-cols-2 gap-4 border-y border-dashed py-4 sm:grid-cols-3 lg:grid-cols-6">
        <KV k={tm("kv.status")} v={<StatusText status={d.status} />} />
        <KV
          k={tm("kv.for")}
          v={d.status_since ? duration(now - d.status_since) : "--"}
        />
        <KV
          k={t("cpu")}
          v={m ? <Metric value={m.cpu_pct} limit={limits.cpu_pct} /> : "--"}
        />
        <KV
          k={t("ram")}
          v={
            m ? (
              <Metric
                value={pctOf(m.mem_used, m.mem_total)}
                limit={limits.mem_pct}
              />
            ) : (
              "--"
            )
          }
        />
        <KV
          k={disk ? `${t("disk")} ${disk.mount}` : t("disk")}
          v={disk ? <Metric value={disk.pct} limit={limits.disk_pct} /> : "--"}
        />
        <KV
          k={t("loadPerCore")}
          v={
            m ? (
              <Metric value={perCore} limit={limits.load} unit="" digits={2} />
            ) : (
              "--"
            )
          }
        />
      </div>

      {locs.length > 1 && (
        <div className="mt-4 flex flex-wrap items-center gap-1 text-xs">
          <span className="mr-1 text-muted-foreground">
            {t("servers", { count: locs.length })}
          </span>
          {locs.map((l) => (
            <button
              key={l.location}
              onClick={() => setLoc(l.location)}
              className={segment({ active: current?.location === l.location })}
            >
              {l.name}
            </button>
          ))}
        </div>
      )}

      <Section title={t("title")}>
        {!data ? (
          <p className="text-xs text-muted-foreground">{t("loading")}</p>
        ) : !current ? (
          <p className="text-xs text-muted-foreground">{t("noLocation")}</p>
        ) : (
          <div className="grid gap-3">
            <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 text-sm">
              <span className="truncate">
                <CountryFlag code={status.get(current.location)?.country} />
                {current.name}
                {status.get(current.location)?.online === false && (
                  <span className="ml-2 text-xs text-warn">{t("offline")}</span>
                )}
              </span>
              <span className="flex gap-3 text-xs text-muted-foreground">
                {m && m.uptime_sec > 0 && (
                  <span>
                    {t("uptime", { time: duration(m.uptime_sec * 1000) })}
                  </span>
                )}
                <span>
                  {current.at
                    ? t("updated", { ago: ago(current.at) })
                    : t("noData")}
                </span>
              </span>
            </div>
            {m ? (
              <Snapshot m={m} limits={limits} />
            ) : (
              <p className="text-xs text-muted-foreground">{t("noDataHint")}</p>
            )}
          </div>
        )}
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
        {data && current ? (
          <History
            data={data}
            location={current.location}
            cores={m?.cores ?? 1}
            limits={limits}
            intervalSec={d.interval_sec}
            hasSwap={(m?.swap_total ?? 0) > 0}
          />
        ) : (
          <div className="h-[260px]" />
        )}
      </Section>
    </>
  )
}
