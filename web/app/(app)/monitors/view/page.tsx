"use client"

import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { Suspense, useState } from "react"
import { toast } from "sonner"

import { Confirm } from "@/components/confirm"
import { ExpiryTable } from "@/components/expiry"
import { PushInfo } from "@/components/push-info"
import { LatencyChart } from "@/components/latency-chart"
import { TagList, useLabels } from "@/components/labels"
import { KV, PageHeader, Section } from "@/components/page-header"
import { OkText, StatusText } from "@/components/status"
import { UptimeBars } from "@/components/uptime-bars"
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
  type Incident,
  type MonitorDetail,
  type ResultRow,
  type Series,
} from "@/lib/api"
import { useNow } from "@/hooks/use-now"
import { ago, datetime, displayTarget, duration, interval, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

const ranges = ["1h", "6h", "24h", "7d", "30d"] as const

function View() {
  const router = useRouter()
  const id = Number(useSearchParams().get("id"))
  const now = useNow()
  const [range, setRange] = useState<(typeof ranges)[number]>("24h")
  const m = useApi<MonitorDetail>(id ? `/api/monitors/${id}` : null)
  const { groupById, tagById } = useLabels()
  const series = useApi<Series>(
    id ? `/api/monitors/${id}/series?range=${range}` : null,
    { refreshInterval: 30_000 }
  )
  const results = useApi<{
    results: ResultRow[]
    locations: Record<string, string>
  }>(id ? `/api/monitors/${id}/results?limit=30` : null)
  const incidents = useApi<Incident[]>(
    id ? `/api/incidents?monitor=${id}` : null,
    { refreshInterval: 30_000 }
  )

  if (m.error) return <div className="text-down">! {m.error.message}</div>
  if (!m.data)
    return (
      <div className="text-muted-foreground">
        loading<span className="cursor-blink">_</span>
      </div>
    )
  const d = m.data

  async function togglePause() {
    try {
      await api(`/api/monitors/${id}/pause`, {
        method: "POST",
        body: { paused: !d.paused },
      })
      toast.success(d.paused ? "monitor resumed" : "monitor paused")
      m.mutate()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "error")
    }
  }

  async function remove() {
    await api(`/api/monitors/${id}`, { method: "DELETE" })
    toast.success("monitor deleted")
    router.push("/")
  }

  return (
    <>
      <PageHeader cmd={`pingtxt monitors show ${id}`} title={d.name}>
        <Button variant="outline" size="sm" onClick={togglePause}>
          {d.paused ? "resume" : "pause"}
        </Button>
        <Button
          variant="outline"
          size="sm"
          render={<Link href={`/monitors/edit/?id=${id}`} />}
          nativeButton={false}
        >
          edit
        </Button>
        <Confirm
          title={`delete ${d.name}?`}
          description="All results and incidents of this monitor will be deleted."
          onConfirm={remove}
          trigger={
            <Button variant="destructive" size="sm">
              delete
            </Button>
          }
        />
      </PageHeader>

      <div className="-mt-4 mb-6 truncate text-xs text-muted-foreground">
        {d.type === "push" ? (
          <>
            heartbeat · expected every{" "}
            <span className="text-foreground">{interval(d.interval_sec)}</span>{" "}
            · grace {d.grace_sec ? interval(d.grace_sec) : "none"}
          </>
        ) : (
          <>
            {d.type} · <span className="text-foreground">{displayTarget(d.target)}</span> ·
            every {interval(d.interval_sec)} · timeout {d.timeout_ms}ms ·
            retries {d.retries}
          </>
        )}
      </div>
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
          ◇ in maintenance: <span className="text-foreground">{d.maintenance.name}</span> until {datetime(d.maintenance.end_at)}. checks
          keep running, alerts and incidents are paused.{" "}
          <Link href="/maintenance/" className="underline underline-offset-4">
            manage
          </Link>
        </div>
      )}

      <div className="grid grid-cols-2 gap-4 border-y border-dashed py-4 sm:grid-cols-3 lg:grid-cols-6">
        <KV k="status" v={<StatusText status={d.status} />} />
        <KV
          k="for"
          v={d.status_since ? duration(now - d.status_since) : "--"}
        />
        <KV k="uptime 24h" v={pct(d.uptime_24h)} />
        <KV k="uptime 7d" v={pct(d.uptime_7d)} />
        <KV k="uptime 30d" v={pct(d.uptime_30d)} />
        <KV k="avg 24h" v={ms(d.avg_ms_24h)} />
      </div>

      {d.type === "push" && d.push_url && (
        <PushInfo id={id} url={d.push_url} onRotated={() => m.mutate()} />
      )}

      <Section title="last 24h">
        <UptimeBars bars={d.bars} height="h-8" />
      </Section>

      {d.expiry.length > 0 && (
        <Section title="expiry">
          <ExpiryTable monitorId={id} checks={d.expiry} now={now} onChange={() => m.mutate()} />
        </Section>
      )}

      <Section
        title={d.type === "push" ? "run time (from ?ping=)" : "response time"}
        actions={
          <div className="flex gap-1 normal-case">
            {ranges.map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={cn(
                  "px-1.5 hover:text-foreground",
                  range === r && "bg-accent text-foreground"
                )}
              >
                {r}
              </button>
            ))}
          </div>
        }
      >
        {series.data ? (
          <LatencyChart series={series.data} />
        ) : (
          <div className="h-[180px]" />
        )}
      </Section>

      <Section title={d.type === "push" ? "last heartbeat" : "locations"}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{d.type === "push" ? "source" : "location"}</TableHead>
              <TableHead>state</TableHead>
              <TableHead className="text-right">latency</TableHead>
              <TableHead>message</TableHead>
              <TableHead className="text-right">checked</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {d.location_status.map((l) => (
              <TableRow key={l.id}>
                <TableCell>{l.name}</TableCell>
                <TableCell>
                  {!l.online ? (
                    <span className="text-muted-foreground">offline</span>
                  ) : l.last ? (
                    <OkText ok={l.last.ok} />
                  ) : (
                    <span className="text-muted-foreground">waiting</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  {ms(l.last?.latency_ms)}
                </TableCell>
                <TableCell className="max-w-72 truncate text-muted-foreground">
                  {l.last?.message}
                </TableCell>
                <TableCell className="text-right text-muted-foreground">
                  {ago(l.last?.ts)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Section>

      <Section title="incidents">
        {incidents.data?.length ? (
          <div className="grid gap-1">
            {incidents.data.slice(0, 10).map((i) => (
              <div key={i.id} className="flex flex-wrap gap-x-3 text-xs">
                <span
                  className={
                    i.resolved_at ? "text-muted-foreground" : "text-down"
                  }
                >
                  {i.resolved_at ? "[resolved]" : "[ongoing] "}
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
            no incidents. nice.
          </div>
        )}
      </Section>

      <Section title="log">
        <div className="max-h-96 overflow-y-auto border bg-muted/30 p-3 text-xs leading-6">
          {(results.data?.results ?? []).map((r) => (
            <div key={r.id} className="flex gap-3 whitespace-nowrap">
              <span className="text-muted-foreground">{datetime(r.ts)}</span>
              <span className="w-24 shrink-0 truncate">
                {results.data?.locations[r.location] ?? r.location}
              </span>
              <span className="w-8 shrink-0">
                <OkText ok={r.ok} />
              </span>
              <span className="w-16 shrink-0 text-right">
                {ms(r.latency_ms)}
              </span>
              <span className="truncate text-muted-foreground">
                {r.maint && <span className="text-info">[maint] </span>}
                {r.message}
              </span>
            </div>
          ))}
          {results.data?.results.length === 0 && (
            <span className="text-muted-foreground">
              waiting for the first check...
            </span>
          )}
        </div>
      </Section>
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
