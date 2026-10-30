"use client"

import Link from "next/link"
import { useMemo, useState } from "react"
import { PlusIcon } from "lucide-react"

import { ExpiryBadges } from "@/components/expiry"
import { TagList, TagToggle, useLabels } from "@/components/labels"
import { Empty, PageHeader } from "@/components/page-header"
import { StatusGlyph, StatusText } from "@/components/status"
import { UptimeBars } from "@/components/uptime-bars"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useMinute } from "@/hooks/use-now"
import { useApi, type MonitorSummary, type Status } from "@/lib/api"
import { ago, displayTarget, interval, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

const order: Record<Status, number> = {
  down: 0,
  degraded: 1,
  pending: 2,
  maintenance: 3,
  up: 4,
  paused: 5,
}
const filters = [
  "all",
  "up",
  "down",
  "degraded",
  "maintenance",
  "paused",
] as const

export default function MonitorsPage() {
  const { data, isLoading } = useApi<MonitorSummary[]>("/api/monitors")
  const [q, setQ] = useState("")
  const [filter, setFilter] = useState<(typeof filters)[number]>("all")
  // "all", "none" (no group) or a group id
  const [group, setGroup] = useState<string>("all")
  const [tag, setTag] = useState<number | null>(null)
  const now = useMinute()
  const { groups, tags, groupById, tagById } = useLabels()
  const hasGroups = (groups.data?.length ?? 0) > 0

  const counts = useMemo(() => {
    const c: Record<string, number> = { all: data?.length ?? 0 }
    for (const m of data ?? []) c[m.status] = (c[m.status] ?? 0) + 1
    return c
  }, [data])

  const list = useMemo(() => {
    const s = q.toLowerCase()
    return (data ?? [])
      .filter((m) => filter === "all" || m.status === filter)
      .filter(
        (m) =>
          group === "all" ||
          (group === "none"
            ? !groupById.has(m.group_id)
            : String(m.group_id) === group)
      )
      .filter((m) => tag === null || m.tag_ids?.includes(tag))
      .filter(
        (m) =>
          !s ||
          m.name.toLowerCase().includes(s) ||
          displayTarget(m.target).toLowerCase().includes(s) ||
          m.type.includes(s) ||
          m.tag_ids?.some((id) =>
            tagById.get(id)?.name.toLowerCase().includes(s)
          ) ||
          groupById.get(m.group_id)?.name.toLowerCase().includes(s)
      )
      .sort(
        (a, b) =>
          order[a.status] - order[b.status] || a.name.localeCompare(b.name)
      )
  }, [data, q, filter, group, tag, groupById, tagById])

  // With groups, the list is split in one section per group (by name), with
  // monitors without a group last.
  const sections = useMemo(() => {
    if (!hasGroups) return [{ key: "all", title: "", items: list }]
    const by = new Map<number, MonitorSummary[]>()
    for (const m of list) {
      const k = groupById.has(m.group_id) ? m.group_id : 0
      by.set(k, [...(by.get(k) ?? []), m])
    }
    const out = (groups.data ?? [])
      .filter((g) => by.has(g.id))
      .map((g) => ({ key: String(g.id), title: g.name, items: by.get(g.id)! }))
    if (by.has(0))
      out.push({ key: "none", title: "ungrouped", items: by.get(0)! })
    return out
  }, [list, hasGroups, groups.data, groupById])

  return (
    <>
      <PageHeader cmd="pingtxt monitors ls" title="monitors">
        <Input
          placeholder="grep..."
          value={q}
          onChange={(e) => setQ(e.target.value)}
          className="h-8 w-40"
        />
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

      {(hasGroups || (tags.data?.length ?? 0) > 0) && (
        <div className="-mt-2 mb-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
          {hasGroups && (
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-muted-foreground">group:</span>
              {[
                { k: "all", label: "all" },
                ...(groups.data ?? []).map((g) => ({
                  k: String(g.id),
                  label: g.name,
                })),
                { k: "none", label: "ungrouped" },
              ].map((g) => (
                <button
                  key={g.k}
                  onClick={() => setGroup(g.k)}
                  className={cn(
                    "px-2 py-0.5 text-muted-foreground hover:text-foreground",
                    group === g.k && "bg-accent text-foreground"
                  )}
                >
                  {g.label}
                </button>
              ))}
            </div>
          )}
          {(tags.data?.length ?? 0) > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-muted-foreground">tag:</span>
              {(tags.data ?? []).map((t) => (
                <TagToggle
                  key={t.id}
                  tag={t}
                  on={tag === t.id}
                  onChange={(on) => setTag(on ? t.id : null)}
                />
              ))}
            </div>
          )}
        </div>
      )}

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
              <Link
                href="/monitors/new/"
                className="text-primary underline underline-offset-4"
              >
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
          {sections.map((sec) => (
            <div key={sec.key}>
              {sec.title && (
                <div className="flex items-center gap-2 border-b border-dashed bg-muted/40 px-2 py-1 text-xs">
                  <span className="font-medium">{sec.title}</span>
                  <span className="text-muted-foreground">
                    {sec.items.length}
                    {sec.items.some((m) => m.status === "down") && (
                      <span className="text-down">
                        {" "}
                        · {
                          sec.items.filter((m) => m.status === "down").length
                        }{" "}
                        down
                      </span>
                    )}
                  </span>
                </div>
              )}
              {sec.items.map((m) => (
                <Link
                  key={m.id}
                  href={`/monitors/view/?id=${m.id}`}
                  className="grid grid-cols-[1fr_auto] items-center gap-x-4 gap-y-1 border-b border-dashed px-2 py-2.5 transition-colors hover:bg-accent/60 md:grid-cols-[1fr_220px_72px_64px_80px]"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <StatusGlyph status={m.status} />
                    <div className="min-w-0">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="truncate font-medium">{m.name}</span>
                        <TagList
                          ids={m.tag_ids}
                          tagById={tagById}
                          className="hidden shrink-0 sm:inline-flex"
                        />
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {m.type === "push"
                          ? `heartbeat · expected every ${interval(m.interval_sec)}`
                          : `${m.type} · ${displayTarget(m.target)} · every ${interval(m.interval_sec)}`}
                        {m.locations.length > 1 &&
                          ` · ${m.locations.length} locations`}
                        <ExpiryBadges checks={m.expiry} now={now} />
                      </div>
                    </div>
                  </div>
                  <UptimeBars
                    bars={m.bars}
                    className="col-span-2 md:col-span-1"
                  />
                  <span
                    className={cn(
                      "text-right",
                      m.uptime_24h >= 0 && m.uptime_24h < 0.99 && "text-warn",
                      m.uptime_24h >= 0 && m.uptime_24h < 0.9 && "text-down"
                    )}
                  >
                    <span className="md:hidden">
                      <StatusText status={m.status} className="mr-2" />
                    </span>
                    {pct(m.uptime_24h)}
                  </span>
                  <span className="hidden text-right text-muted-foreground md:block">
                    {ms(m.avg_ms_24h)}
                  </span>
                  <span className="hidden text-right text-xs text-muted-foreground md:block">
                    {m.paused ? "paused" : ago(m.last?.ts)}
                  </span>
                </Link>
              ))}
            </div>
          ))}
        </div>
      )}
    </>
  )
}
