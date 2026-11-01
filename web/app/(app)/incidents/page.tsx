"use client"

import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { Suspense } from "react"
import { useTranslations } from "use-intl"

import { Empty, PageHeader } from "@/components/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi, type Incident, type MonitorSummary } from "@/lib/api"
import { useMinute, useNow } from "@/hooks/use-now"
import { datetime, duration } from "@/lib/format"
import { segment } from "@/components/ui/control"

const states = ["all", "open", "resolved"] as const
const periods = ["24h", "7d", "30d", "all"] as const
const periodMs: Record<(typeof periods)[number], number> = {
  "24h": 86400_000,
  "7d": 7 * 86400_000,
  "30d": 30 * 86400_000,
  all: 0,
}

export default function IncidentsPage() {
  return (
    <Suspense>
      <Incidents />
    </Suspense>
  )
}

function Incidents() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname() || "/incidents/"
  const st = params.get("state")
  const state = states.includes(st as (typeof states)[number]) ? (st as (typeof states)[number]) : "all"
  const pe = params.get("period")
  const period = periods.includes(pe as (typeof periods)[number]) ? (pe as (typeof periods)[number]) : "all"
  const monitor = Number(params.get("monitor")) || 0
  const minute = useMinute()
  const now = useNow()
  const t = useTranslations("incidents")

  const q = new URLSearchParams({ limit: "200" })
  if (state !== "all") q.set("state", state)
  if (monitor) q.set("monitor", String(monitor))
  // Rounded to the minute so the request (and its cache key) stays stable.
  if (periodMs[period] && minute) q.set("since", String(minute - periodMs[period]))
  const { data } = useApi<Incident[]>(`/api/incidents?${q}`)
  const monitors = useApi<MonitorSummary[]>("/api/monitors", { refreshInterval: 60_000 })
  const monitorItems = [
    { value: "0", label: t("allMonitors") },
    ...[...(monitors.data ?? [])]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((m) => ({ value: String(m.id), label: m.name })),
  ]

  function set(key: string, value: string | null) {
    const next = new URLSearchParams(window.location.search)
    if (!value) next.delete(key)
    else next.set(key, value)
    const qs = next.toString()
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
  }
  const filtered = state !== "all" || period !== "all" || monitor > 0

  return (
    <>
      <PageHeader cmd="pingtxt incidents ls" title={t("title")}>
        <Select
          items={monitorItems}
          value={String(monitor)}
          onValueChange={(v) => set("monitor", v && v !== "0" ? v : null)}
        >
          <SelectTrigger className="w-48" aria-label={t("monitor")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {monitorItems.map((m) => (
              <SelectItem key={m.value} value={m.value}>
                {m.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </PageHeader>

      <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
        <div className="flex flex-wrap items-center gap-1">
          {states.map((s) => (
            <button
              key={s}
              onClick={() => set("state", s === "all" ? null : s)}
              className={segment({ active: state === s })}
            >
              {t(`filter.${s}`)}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-1">
          <span className="text-muted-foreground">{t("filter.period")}</span>
          {periods.map((p) => (
            <button
              key={p}
              onClick={() => set("period", p === "all" ? null : p)}
              className={segment({ active: period === p })}
            >
              {p === "all" ? t("filter.anyTime") : p}
            </button>
          ))}
        </div>
        {data && (
          <span className="text-muted-foreground">{t("count", { count: data.length })}</span>
        )}
      </div>

      {data && data.length === 0 ? (
        <Empty>{filtered ? t("noMatch") : t("empty")}</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("state")}</TableHead>
              <TableHead>{t("monitor")}</TableHead>
              <TableHead>{t("started")}</TableHead>
              <TableHead>{t("duration")}</TableHead>
              <TableHead className="hidden md:table-cell">{t("cause")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(data ?? []).map((i) => {
              // The monitor's check log around the incident.
              const to = (i.resolved_at || now) + 60_000
              return (
                <TableRow key={i.id}>
                  <TableCell>
                    {i.resolved_at ? (
                      <span className="text-up">{t("resolved")}</span>
                    ) : (
                      <span className="animate-pulse text-down">{t("ongoing")}</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <Link href={`/monitors/view/?id=${i.monitor_id}`} className="underline-offset-4 hover:underline">
                      {i.monitor_name}
                    </Link>
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    <Link
                      href={`/monitors/view/?id=${i.monitor_id}&from=${i.started_at - 60_000}&to=${to}#log`}
                      className="underline-offset-4 hover:underline"
                      title={t("showChecks")}
                    >
                      {datetime(i.started_at)}
                    </Link>
                  </TableCell>
                  <TableCell title={i.resolved_at ? datetime(i.resolved_at) : undefined}>
                    {duration((i.resolved_at || now) - i.started_at)}
                  </TableCell>
                  <TableCell className="hidden max-w-md truncate text-muted-foreground md:table-cell" title={i.cause}>
                    {i.cause}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </>
  )
}
