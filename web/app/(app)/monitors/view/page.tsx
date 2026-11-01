"use client"

import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { Suspense, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { MonitorBadges } from "@/components/badge-info"
import { ExpiryTable } from "@/components/expiry"
import { PushInfo } from "@/components/push-info"
import { LatencyChart } from "@/components/latency-chart"
import { PeriodStatsRow } from "@/components/period-stats"
import { PinButton } from "@/components/pin-button"
import { PhaseChart, hasPhases, phaseLocations, phaseTitle } from "@/components/phase-chart"
import { TagList, useLabels } from "@/components/labels"
import { KV, PageHeader, Section } from "@/components/page-header"
import { OkText, StatusText } from "@/components/status"
import { UptimeBars } from "@/components/uptime-bars"
import { ResultsLog, type TimeWindow } from "@/components/results-log"
import { TestResults, type TestResult } from "@/components/test-results"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  api,
  useApi,
  useIsAdmin,
  type Incident,
  type MonitorDetail,
  type Series,
} from "@/lib/api"
import { useNow } from "@/hooks/use-now"
import { deleteWithUndo } from "@/lib/pending-delete"
import { usePins } from "@/lib/pins"
import { ago, datetime, displayTarget, duration, interval, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

const ranges = ["1h", "6h", "24h", "7d", "30d"] as const
type Range = (typeof ranges)[number]

function View() {
  const router = useRouter()
  const params = useSearchParams()
  const id = Number(params.get("id"))
  const now = useNow()
  // Chart window, kept in the URL: ?range=7d, or ?start=&end= after a zoom.
  const rangeParam = params.get("range")
  const range: Range = ranges.includes(rangeParam as Range) ? (rangeParam as Range) : "24h"
  const zStart = Number(params.get("start"))
  const zEnd = Number(params.get("end"))
  const zoom = zStart > 0 && zEnd > zStart ? { start: zStart, end: zEnd } : null
  function setQuery(changes: Record<string, string | null>) {
    const q = new URLSearchParams(params.toString())
    for (const [k, v] of Object.entries(changes)) {
      if (v === null) q.delete(k)
      else q.set(k, v)
    }
    router.replace(`?${q.toString()}`, { scroll: false })
  }
  const [byPhase, setByPhase] = useState(false)
  const [phaseLoc, setPhaseLoc] = useState("")
  const [testing, setTesting] = useState(false)
  // Slot of the uptime bars whose checks the log shows (?from=&to= from the dashboard).
  const [slot, setSlot] = useState<TimeWindow | null>(() => {
    const from = Number(params.get("from"))
    const to = Number(params.get("to"))
    return from > 0 && to > from ? { from, to } : null
  })
  const [tested, setTested] = useState<TestResult[] | null>(null)
  const m = useApi<MonitorDetail>(id ? `/api/monitors/${id}` : null)
  const isAdmin = useIsAdmin()
  const pins = usePins()
  const { groupById, tagById } = useLabels()
  const t = useTranslations("monitor")
  const tc = useTranslations("common")
  const tp = useTranslations("phaseChart")
  const series = useApi<Series>(
    !id ? null : zoom ? `/api/monitors/${id}/series?start=${zoom.start}&end=${zoom.end}` : `/api/monitors/${id}/series?range=${range}`,
    // A zoomed window is in the past: nothing new to fetch.
    { refreshInterval: zoom ? 0 : 30_000, keepPreviousData: true }
  )
  const incidents = useApi<Incident[]>(
    id ? `/api/incidents?monitor=${id}` : null,
    { refreshInterval: 30_000 }
  )

  if (m.error) return <div className="text-down">! {m.error.message}</div>
  if (!m.data)
    return (
      <div className="text-muted-foreground">
        {tc("loading")}<span className="cursor-blink">_</span>
      </div>
    )
  const d = m.data

  async function setPaused(paused: boolean, undo = true) {
    try {
      await api(`/api/monitors/${id}/pause`, { method: "POST", body: { paused } })
      m.mutate()
      if (!undo) {
        toast(t("undone"))
        return
      }
      toast.success(paused ? t("paused") : t("resumed"), {
        action: { label: t("undo"), onClick: () => setPaused(!paused, false) },
      })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("error"))
    }
  }

  async function checkNow() {
    setTesting(true)
    setTested(null)
    try {
      const res = await api<{ results: TestResult[] }>(`/api/monitors/${id}/test`, {
        method: "POST",
      })
      setTested(res.results)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("error"))
    } finally {
      setTesting(false)
    }
  }

  function remove() {
    deleteWithUndo([id], {
      done: t("deleted"),
      undo: t("undo"),
      undone: t("undone"),
      error: t("error"),
    })
    router.push("/")
  }


  return (
    <>
      <PageHeader cmd={`pingtxt monitors show ${id}`} title={d.name}>
        <PinButton id={id} name={d.name} pinned={pins.has(id)} className="h-8 border" />
        {isAdmin && (
          <>
            {d.type !== "push" && (
              <Button variant="outline" size="sm" onClick={checkNow} disabled={testing}>
                {testing ? t("checking") : t("checkNow")}
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={() => setPaused(!d.paused)}>
              {d.paused ? t("resume") : t("pause")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              render={<Link href={`/monitors/edit/?id=${id}`} />}
              nativeButton={false}
            >
              {t("edit")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              render={<Link href={`/monitors/new/?from=${id}`} />}
              nativeButton={false}
              title={t("duplicateHint")}
            >
              {t("duplicate")}
            </Button>
            <Button variant="destructive" size="sm" onClick={remove} title={t("deleteHint")}>
              {tc("delete")}
            </Button>
          </>
        )}
      </PageHeader>

      <div className="-mt-4 mb-6 truncate text-xs text-muted-foreground">
        {d.type === "push" ? (
          <>
            {t.rich("pushLine", {
              every: interval(d.interval_sec),
              grace: d.grace_sec ? interval(d.grace_sec) : t("none"),
              hl: (c) => <span className="text-foreground">{c}</span>,
            })}
          </>
        ) : (
          <>
            {t.rich("checkLine", {
              type: d.type,
              target: displayTarget(d.target),
              every: interval(d.interval_sec),
              timeout: d.timeout_ms,
              retries: d.retries,
              hl: (c) => <span className="text-foreground">{c}</span>,
            })}
            {d.locations.length > 1 &&
              ` · ${
                d.degraded_pct
                  ? t("degradedAt", { pct: d.degraded_pct })
                  : t("degradedAtDefault")
              }`}
            {d.ip_family === 4 || d.ip_family === 6 || d.ip_family === 46
              ? ` · ${t(d.ip_family === 4 ? "ipv4Only" : d.ip_family === 6 ? "ipv6Only" : "ipBoth")}`
              : ""}
          </>
        )}
      </div>
      <TestResults
        results={tested}
        running={testing}
        onClose={() => setTested(null)}
        className="-mt-2 mb-6"
      />
      {(groupById.has(d.group_id) || (d.tag_ids?.length ?? 0) > 0) && (
        <div className="-mt-4 mb-6 flex flex-wrap items-center gap-2 text-xs">
          {groupById.has(d.group_id) && (
            <Link href="/groups/" className="text-muted-foreground hover:text-foreground">
              [{groupById.get(d.group_id)!.name}]
            </Link>
          )}
          <TagList ids={d.tag_ids} tagById={tagById} />
        </div>
      )}

      {d.maintenance && (
        <div className="mb-4 border border-dashed border-info/60 px-3 py-2 text-xs text-info">
          ◇{" "}
          {t.rich("inMaintenance", {
            name: d.maintenance.name,
            until: datetime(d.maintenance.end_at),
            hl: (c) => <span className="text-foreground">{c}</span>,
          })}{" "}
          <Link href="/maintenance/" className="underline underline-offset-4">
            {t("manage")}
          </Link>
        </div>
      )}

      <div className="grid grid-cols-2 gap-4 border-y border-dashed py-4 sm:grid-cols-3 lg:grid-cols-6">
        <KV k={t("kv.status")} v={<StatusText status={d.status} />} />
        <KV
          k={t("kv.for")}
          v={d.status_since ? duration(now - d.status_since) : "--"}
        />
        <KV k={t("kv.uptime24h")} v={pct(d.uptime_24h)} />
        <KV k={t("kv.uptime7d")} v={pct(d.uptime_7d)} />
        <KV k={t("kv.uptime30d")} v={pct(d.uptime_30d)} />
        <KV k={t("kv.avg24h")} v={ms(d.avg_ms_24h)} />
      </div>

      {d.type === "push" && d.push_url && (
        <PushInfo id={id} url={d.push_url} onRotated={() => m.mutate()} />
      )}

      <Section title={t("last24h")}>
        <UptimeBars
          bars={d.bars}
          height="h-8"
          selected={slot?.from}
          onSelect={(from, to) => {
            setSlot(slot?.from === from ? null : { from, to })
            document.getElementById("log")?.scrollIntoView({ behavior: "smooth", block: "start" })
          }}
        />
      </Section>

      {d.expiry.length > 0 && (
        <Section title={t("expiry")}>
          <ExpiryTable monitorId={id} checks={d.expiry} now={now} onChange={() => m.mutate()} />
        </Section>
      )}

      <Section
        title={d.type === "push" ? t("runTime") : t("responseTime")}
        actions={
          <div className="flex gap-1 normal-case">
            {ranges.map((r) => (
              <button
                key={r}
                onClick={() => setQuery({ range: r === "24h" ? null : r, start: null, end: null })}
                className={cn(
                  "px-1.5 hover:text-foreground",
                  !zoom && range === r && "bg-accent text-foreground"
                )}
              >
                {r}
              </button>
            ))}
          </div>
        }
      >
        {hasPhases(series.data) && (
          <div className="mb-2 flex flex-wrap gap-1 text-xs text-muted-foreground">
            {([false, true] as const).map((p) => (
              <button
                key={String(p)}
                onClick={() => setByPhase(p)}
                className={cn(
                  "px-1.5 hover:text-foreground",
                  byPhase === p && "bg-accent text-foreground"
                )}
              >
                {p ? t("byPhase") : t("byLocation")}
              </button>
            ))}
            {byPhase && series.data && phaseLocations(series.data).length > 1 && (
              <>
                <span className="px-1">|</span>
                {[{ id: "", name: tp("allLocations") }, ...phaseLocations(series.data)].map((l) => (
                  <button
                    key={l.id}
                    onClick={() => setPhaseLoc(l.id)}
                    className={cn(
                      "px-1.5 hover:text-foreground",
                      phaseLoc === l.id && "bg-accent text-foreground"
                    )}
                  >
                    {l.name}
                  </button>
                ))}
              </>
            )}
          </div>
        )}
        {series.data && byPhase && hasPhases(series.data) ? (
          <PhaseChart series={series.data} location={phaseLoc} />
        ) : series.data ? (
          <LatencyChart
            series={series.data}
            live={!zoom}
            onZoom={(start, end) => setQuery({ start: String(start), end: String(end) })}
          />
        ) : (
          <div className="h-[180px]" />
        )}
        <div className="mt-1 flex flex-wrap items-center gap-x-3 text-[11px] text-muted-foreground">
          {zoom ? (
            <>
              <span className="text-foreground">
                {t("zoomed", { from: datetime(zoom.start), to: datetime(zoom.end) })}
              </span>
              <button
                onClick={() => setQuery({ start: null, end: null })}
                className="underline underline-offset-4 hover:text-foreground"
              >
                {t("zoomReset", { range })}
              </button>
            </>
          ) : (
            !byPhase && <span className="hidden sm:inline">{t("zoomHint")}</span>
          )}
        </div>
        {series.data?.stats && (
          <PeriodStatsRow current={series.data.stats.current} previous={series.data.stats.previous} />
        )}
      </Section>

      <Section title={d.type === "push" ? t("lastHeartbeat") : t("locations")}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{d.type === "push" ? t("cols.source") : t("cols.location")}</TableHead>
              <TableHead>{t("cols.state")}</TableHead>
              <TableHead className="text-right">{t("cols.latency")}</TableHead>
              <TableHead>{t("cols.message")}</TableHead>
              <TableHead className="text-right">{t("cols.checked")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {d.location_status.map((l) => (
              <TableRow key={l.id}>
                <TableCell>{l.name}</TableCell>
                <TableCell>
                  {!l.online ? (
                    <span className="text-muted-foreground">{t("offline")}</span>
                  ) : l.last ? (
                    <OkText ok={l.last.ok} />
                  ) : (
                    <span className="text-muted-foreground">{t("waiting")}</span>
                  )}
                </TableCell>
                <TableCell
                  className="text-right"
                  title={l.last ? phaseTitle(l.last.timing, l.last.latency_ms, tp) : undefined}
                >
                  {ms(l.last?.latency_ms)}
                </TableCell>
                <TableCell className="max-w-72 truncate text-muted-foreground">
                  {l.last?.message}
                </TableCell>
                <TableCell
                  className="text-right text-muted-foreground"
                  title={l.last?.ts ? datetime(l.last.ts) : undefined}
                >
                  {ago(l.last?.ts)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Section>

      <Section title={t("incidents")}>
        {incidents.data?.length ? (
          <div className="grid gap-1">
            {incidents.data.slice(0, 10).map((i) => (
              <div key={i.id} className="flex flex-wrap gap-x-3 text-xs">
                <span
                  className={
                    i.resolved_at ? "text-muted-foreground" : "text-down"
                  }
                >
                  {i.resolved_at ? `[${t("resolved")}]` : `[${t("ongoing")}] `}
                </span>
                <span>{datetime(i.started_at)}</span>
                <span className="text-muted-foreground">
                  {duration((i.resolved_at || now) - i.started_at)}
                </span>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {i.cause}
                </span>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-xs text-muted-foreground">
            {t("noIncidents")}
          </div>
        )}
      </Section>

      <MonitorBadges monitorId={id} groupId={d.group_id} />

      <ResultsLog
        monitorId={id}
        slot={slot}
        onClearWindow={() => setSlot(null)}
        waitingText={t("waitingFirst")}
      />
    </>
  )
}

export default function MonitorPage() {
  return (
    <Suspense>
      <View />
    </Suspense>
  )
}
