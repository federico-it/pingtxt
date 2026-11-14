"use client"

import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { Suspense, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { CountryFlag } from "@/components/country"
import { MonitorBadges } from "@/components/badge-info"
import { AuditTrail } from "@/components/settings/audit-log"
import { ExpiryTable } from "@/components/expiry"
import { PushInfo } from "@/components/push-info"
import { LatencyChart } from "@/components/latency-chart"
import { PeriodStatsRow } from "@/components/period-stats"
import { PinButton } from "@/components/pin-button"
import { PhaseChart, hasPhases, phaseLocations, phaseTitle } from "@/components/phase-chart"
import { TagList, useLabels } from "@/components/labels"
import { MonitorIcon } from "@/components/monitor-icon"
import { KV, PageHeader, Section } from "@/components/page-header"
import { OkText, StatusGlyph, StatusText } from "@/components/status"
import { UptimeBars } from "@/components/uptime-bars"
import { ServerView } from "@/components/server-metrics"
import { ResultsLog, type TimeWindow } from "@/components/results-log"
import { IncidentDiagnostics } from "@/components/diagnostics"
import { ReasonList, parseCause } from "@/components/reasons"
import { AckButton, AckNote, isSnoozed, SnoozeButton, SnoozeNote } from "@/components/incident-ack"
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
import { segment } from "@/components/ui/control"
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
  const ts = useTranslations("status")
  const series = useApi<Series>(
    // Server monitors have no response time chart.
    !id || m.data?.type === "server" ? null : zoom ? `/api/monitors/${id}/series?start=${zoom.start}&end=${zoom.end}` : `/api/monitors/${id}/series?range=${range}`,
    // A zoomed window is in the past: nothing new to fetch.
    { refreshInterval: zoom ? 0 : 30_000, keepPreviousData: true }
  )
  const incidents = useApi<Incident[]>(
    id ? `/api/incidents?monitor=${id}` : null,
    { refreshInterval: 30_000 }
  )
  const [openDiag, setOpenDiag] = useState(0)

  if (m.error) return <div className="text-down">! {m.error.message}</div>
  if (!m.data)
    return (
      <div className="text-muted-foreground">
        {tc("loading")}<span className="cursor-blink">_</span>
      </div>
    )
  const d = m.data
  // Slow threshold in use at each location (fixed or automatic).
  const slow = Object.fromEntries(
    d.location_status.filter((l) => l.slow_ms > 0).map((l) => [l.id, l.slow_ms])
  )

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
      <PageHeader cmd={`pingtxt monitors show ${id}`} title={
          d.icon_url ? (
            <span className="flex min-w-0 items-center gap-2">
              <MonitorIcon m={d} className="size-6" />
              <span className="truncate">{d.name}</span>
            </span>
          ) : (
            d.name
          )
        }>
        <PinButton id={id} name={d.name} pinned={pins.has(id)} className="size-8 self-auto border" />
        {isAdmin && (
          <>
            {d.type !== "push" && (
              <Button variant="outline" onClick={checkNow} disabled={testing}>
                {testing ? t("checking") : t("checkNow")}
              </Button>
            )}
            <Button variant="outline" onClick={() => setPaused(!d.paused)}>
              {d.paused ? t("resume") : t("pause")}
            </Button>
            <Button
              variant="outline"
              render={<Link href={`/monitors/edit/?id=${id}`} />}
              nativeButton={false}
            >
              {t("edit")}
            </Button>
            <Button
              variant="outline"
              render={<Link href={`/monitors/new/?from=${id}`} />}
              nativeButton={false}
              title={t("duplicateHint")}
            >
              {t("duplicate")}
            </Button>
            <Button variant="destructive" onClick={remove} title={t("deleteHint")}>
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
        ) : d.type === "server" ? (
          <>
            {t.rich("serverLine", {
              where: d.location_status.map((l) => l.name).join(", ") || "--",
              every: interval(d.interval_sec),
              retries: d.retries,
              hl: (c) => <span className="text-foreground">{c}</span>,
            })}
            {d.target.trim() && ` · ${t("serverDisks", { disks: d.target })}`}
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
            {d.slow_ms > 0 && ` · ${t("slowAt", { ms: d.slow_ms })}`}
            {d.slow_ms === -1 && ` · ${t("slowAuto")}`}
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
              [{groupById.get(d.group_id)!.path}]
            </Link>
          )}
          <TagList ids={d.tag_ids} tagById={tagById} />
        </div>
      )}

      {!d.paused && (d.status === "degraded" || d.status === "down") && (d.reasons?.length ?? 0) > 0 && (
        <div
          className={cn(
            "mb-4 border border-dashed px-3 py-2 text-xs",
            d.status === "down" ? "border-down/60" : "border-warn/60"
          )}
        >
          <div className={cn("mb-1 uppercase tracking-wide", d.status === "down" ? "text-down" : "text-warn")}>
            <StatusGlyph status={d.status} /> {ts(d.status)}
          </div>
          <ReasonList reasons={d.reasons!} total={d.location_status.length} omitAll className="text-sm" />
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

      {d.blocked_by && (
        <div className="mb-4 border border-dashed px-3 py-2 text-xs text-muted-foreground">
          ⛓{" "}
          {t.rich("blockedBy", {
            name: d.blocked_by.name,
            since: datetime(d.blocked_by.since),
            hl: (c) => (
              <Link href={`/monitors/view/?id=${d.blocked_by!.id}`} className="text-foreground underline underline-offset-4">
                {c}
              </Link>
            ),
          })}
        </div>
      )}

      {d.incident && !d.paused && (
        <div className="mb-4 flex flex-wrap items-center gap-x-3 gap-y-1.5 border border-dashed border-down/60 px-3 py-2 text-xs">
          <span className="text-down">
            {t("openIncident", { since: datetime(d.incident.started_at) })}
          </span>
          {d.incident.acked_at ? (
            <AckNote incident={d.incident} />
          ) : isSnoozed(d.incident) ? null : (
            <span className="text-muted-foreground">{t("notAcked")}</span>
          )}
          <SnoozeNote incident={d.incident} />
          <span className="ml-auto flex flex-wrap gap-1.5">
            <SnoozeButton
              incident={d.incident}
              onChange={() => {
                m.mutate()
                incidents.mutate()
              }}
            />
            <AckButton
              incident={d.incident}
              onChange={() => {
                m.mutate()
                incidents.mutate()
              }}
            />
          </span>
        </div>
      )}


      {d.type === "server" ? (
        <ServerView d={d} now={now} />
      ) : (
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
      )}

      {d.type === "push" && d.push_url && (
        <PushInfo id={id} url={d.push_url} onRotated={() => m.mutate()} />
      )}

      <Section title={d.type === "server" ? t("serverChecks") : t("last24h")}>
        <UptimeBars
          bars={d.bars}
          height="h-8"
          selected={slot?.from}
          onSelect={(from, to) => {
            setSlot(slot?.from === from ? null : { from, to })
            document.getElementById("log")?.scrollIntoView({ behavior: "smooth", block: "start" })
          }}
        />
        {d.type === "server" && (
          <div className="mt-2 text-xs text-muted-foreground">
            {t("withinLimits", { d1: pct(d.uptime_24h), d7: pct(d.uptime_7d), d30: pct(d.uptime_30d) })}
          </div>
        )}
      </Section>

      {d.expiry.length > 0 && (
        <Section title={t("expiry")}>
          <ExpiryTable monitorId={id} checks={d.expiry} now={now} onChange={() => m.mutate()} />
        </Section>
      )}

      {d.type !== "server" && (
      <Section
        title={d.type === "push" ? t("runTime") : t("responseTime")}
        actions={
          <div className="flex gap-1 normal-case">
            {ranges.map((r) => (
              <button
                key={r}
                onClick={() => setQuery({ range: r === "24h" ? null : r, start: null, end: null })}
                className={segment({ active: !zoom && range === r })}
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
                className={segment({ active: byPhase === p })}
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
                    className={segment({ active: phaseLoc === l.id })}
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
            slow={slow}
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
      )}

      {d.type !== "server" && (
      <Section title={d.type === "push" ? t("lastHeartbeat") : t("locations")}>
        {d.type !== "push" && d.location_status.length > 0 && d.location_status.every((l) => !l.online) && (
          <p className="mb-2 text-xs text-warn">{t("noLocationOnline")}</p>
        )}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{d.type === "push" ? t("cols.source") : t("cols.location")}</TableHead>
              <TableHead>{t("cols.state")}</TableHead>
              <TableHead className="text-right">{t("cols.latency")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {d.location_status.map((l) => {
              const st = !l.online
                ? "paused"
                : !l.last
                  ? "pending"
                  : !l.last.ok
                    ? "down"
                    : l.degraded
                      ? "degraded"
                      : "up"
              // The message is in the box at the top when it explains the
              // monitor's status: show it here only when it adds something.
              const note =
                (st === "down" || st === "degraded") &&
                l.last?.message &&
                !d.reasons?.some((r) => r.message === l.last!.message)
                  ? l.last.message
                  : ""
              return (
                <TableRow key={l.id} title={l.last?.message || undefined}>
                  <TableCell className="whitespace-normal">
                    <StatusGlyph status={st} className="mr-2" />
                    <CountryFlag code={l.country} />
                    {l.name}
                    {note && (
                      <div className="mt-1 max-w-[65vw] whitespace-normal pl-5 text-xs text-muted-foreground [overflow-wrap:anywhere] md:max-w-xl">
                        {note}
                      </div>
                    )}
                  </TableCell>
                  <TableCell title={l.last?.ts ? `${datetime(l.last.ts)} · ${ago(l.last.ts)}` : undefined}>
                    {!l.online ? (
                      <span className="text-muted-foreground">{t("offline")}</span>
                    ) : l.last?.ok && l.degraded ? (
                      <span className="text-warn">{ts("degraded")}</span>
                    ) : l.last ? (
                      <OkText ok={l.last.ok} />
                    ) : (
                      <span className="text-muted-foreground">{t("waiting")}</span>
                    )}
                  </TableCell>
                  <TableCell
                    className="text-right text-muted-foreground"
                    title={l.last ? phaseTitle(l.last.timing, l.last.latency_ms, tp) : undefined}
                  >
                    {ms(l.last?.latency_ms)}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </Section>
      )}

      <Section title={t("incidents")}>
        {incidents.data?.length ? (
          <div className="grid text-xs">
            {incidents.data.slice(0, 10).map((i) => (
              <div key={i.id} className="min-w-0 border-b border-dashed py-2.5 first:pt-0 last:border-0">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span className={i.resolved_at ? "text-muted-foreground" : "text-down"}>
                    [{i.resolved_at ? t("resolved") : t("ongoing")}]
                  </span>
                  {!i.resolved_at && !!i.acked_at && (
                    <AckNote incident={{ id: i.id, acked_at: i.acked_at, acked_by: i.acked_by ?? "" }} />
                  )}
                  <span>{datetime(i.started_at)}</span>
                  <span className="text-muted-foreground">
                    {duration((i.resolved_at || now) - i.started_at)}
                  </span>
                  {i.diagnostics && (
                    <button
                      onClick={() => setOpenDiag(openDiag === i.id ? 0 : i.id)}
                      className={segment({ active: openDiag === i.id, size: "xs", className: "ml-auto" })}
                      aria-expanded={openDiag === i.id}
                    >
                      {t("diagnostics")}
                    </button>
                  )}
                </div>
                <ReasonList
                  reasons={parseCause(i.cause)}
                  total={d.locations.length}
                  className="mt-1.5 pl-3 text-muted-foreground"
                />
                {openDiag === i.id && (
                  <div className="mt-2 border border-dashed p-3">
                    <IncidentDiagnostics incident={i.id} />
                  </div>
                )}
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

      <AuditTrail type="monitor" id={id} />

      <ResultsLog
        monitorId={id}
        slot={slot}
        onClearWindow={() => setSlot(null)}
        waitingText={t("waitingFirst")}
        slow={slow}
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

