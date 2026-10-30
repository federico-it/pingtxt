"use client"

import Link from "next/link"
import { useMemo, useState } from "react"
import { PlusIcon } from "lucide-react"

import { Empty, PageHeader } from "@/components/page-header"
import { StatusGlyph, StatusText } from "@/components/status"
import { UptimeBars } from "@/components/uptime-bars"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useApi, type MonitorSummary, type Status } from "@/lib/api"
import { ago, interval, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

const order: Record<Status, number> = { down: 0, degraded: 1, pending: 2, maintenance: 3, up: 4, paused: 5 }
const filters = ["all", "up", "down", "degraded", "maintenance", "paused"] as const

export default function MonitorsPage() {
  const { data, isLoading } = useApi<MonitorSummary[]>("/api/monitors")
  const [q, setQ] = useState("")
  const [filter, setFilter] = useState<(typeof filters)[number]>("all")

  const counts = useMemo(() => {
    const c: Record<string, number> = { all: data?.length ?? 0 }
    for (const m of data ?? []) c[m.status] = (c[m.status] ?? 0) + 1
    return c
  }, [data])

  const list = useMemo(() => {
    const s = q.toLowerCase()
    return (data ?? [])
      .filter((m) => filter === "all" || m.status === filter)
      .filter((m) => !s || m.name.toLowerCase().includes(s) || m.target.toLowerCase().includes(s))
      .sort((a, b) => order[a.status] - order[b.status] || a.name.localeCompare(b.name))
  }, [data, q, filter])

  return (
    <>
      <PageHeader cmd="pingtxt monitors ls" title="monitors">
        <Input placeholder="grep..." value={q} onChange={(e) => setQ(e.target.value)} className="h-8 w-40" />
        <Button render={<Link href="/monitors/new/" />} nativeButton={false}>
          <PlusIcon /> new
        </Button>
      </PageHeader>

      <div className="mb-4 flex flex-wrap gap-1 text-xs">
        {filters.map((f) => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={cn(
              "px-2 py-0.5 text-muted-foreground hover:text-foreground",
              filter === f && "bg-accent text-foreground"
            )}
          >
            {f}:{counts[f] ?? 0}
          </button>
        ))}
      </div>

      {isLoading && !data ? (
        <div className="text-muted-foreground">
          fetching<span className="cursor-blink">_</span>
        </div>
      ) : list.length === 0 ? (
        <Empty>
          {data?.length ? (
            "no monitors match"
          ) : (
            <>
              no monitors yet.{" "}
              <Link href="/monitors/new/" className="text-primary underline underline-offset-4">
                create the first one
              </Link>
            </>
          )}
        </Empty>
      ) : (
        <div className="border-t border-dashed">
          <div className="hidden grid-cols-[1fr_220px_72px_64px_80px] gap-4 border-b border-dashed px-2 py-1.5 text-[11px] text-muted-foreground uppercase md:grid">
            <span>name</span>
            <span>last 24h</span>
            <span className="text-right">uptime</span>
            <span className="text-right">avg</span>
            <span className="text-right">checked</span>
          </div>
          {list.map((m) => (
            <Link
              key={m.id}
              href={`/monitors/view/?id=${m.id}`}
              className="grid grid-cols-[1fr_auto] items-center gap-x-4 gap-y-1 border-b border-dashed px-2 py-2.5 transition-colors hover:bg-accent/60 md:grid-cols-[1fr_220px_72px_64px_80px]"
            >
              <div className="flex min-w-0 items-center gap-2">
                <StatusGlyph status={m.status} />
                <div className="min-w-0">
                  <div className="truncate font-medium">{m.name}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {m.type} · {m.target} · every {interval(m.interval_sec)}
                    {m.locations.length > 1 && ` · ${m.locations.length} locations`}
                  </div>
                </div>
              </div>
              <UptimeBars bars={m.bars} className="col-span-2 md:col-span-1" />
              <span className={cn("text-right", m.uptime_24h >= 0 && m.uptime_24h < 0.99 && "text-warn", m.uptime_24h >= 0 && m.uptime_24h < 0.9 && "text-down")}>
                <span className="md:hidden">
                  <StatusText status={m.status} className="mr-2" />
                </span>
                {pct(m.uptime_24h)}
              </span>
              <span className="hidden text-right text-muted-foreground md:block">{ms(m.avg_ms_24h)}</span>
              <span className="hidden text-right text-xs text-muted-foreground md:block">{m.paused ? "paused" : ago(m.last?.ts)}</span>
            </Link>
          ))}
        </div>
      )}
    </>
  )
}
