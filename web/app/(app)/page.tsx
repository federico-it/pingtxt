"use client"

import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { Suspense, useCallback, useEffect, useMemo, useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"


import { ExpiryBadges } from "@/components/expiry"
import { TagList, TagToggle, useLabels } from "@/components/labels"
import { Empty, PageHeader } from "@/components/page-header"
import { StatusGlyph, StatusText } from "@/components/status"
import { UptimeBars } from "@/components/uptime-bars"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { useMinute } from "@/hooks/use-now"
import { useHotkey } from "@/hooks/use-hotkey"
import { api, useApi, useIsAdmin, type MonitorSummary, type Status } from "@/lib/api"
import { ago, datetime, displayTarget, interval, ms, pct } from "@/lib/format"
import { deleteWithUndo, usePendingDeletes } from "@/lib/pending-delete"
import { usePins } from "@/lib/pins"
import { PinButton } from "@/components/pin-button"
import { segment } from "@/components/ui/control"
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
type Filter = (typeof filters)[number]

const sorts = ["status", "name", "uptime", "latency", "changed"] as const
type Sort = (typeof sorts)[number]

// Compares two monitors for a sort order; ties fall back to status, then name.
function compare(sort: Sort, a: MonitorSummary, b: MonitorSummary) {
  const byStatus = order[a.status] - order[b.status]
  const byName = a.name.localeCompare(b.name)
  // Monitors without data (-1 uptime, 0 latency) go last.
  const up = (m: MonitorSummary) => (m.uptime_24h < 0 ? 2 : m.uptime_24h)
  const lat = (m: MonitorSummary) => (m.avg_ms_24h > 0 ? m.avg_ms_24h : -1)
  switch (sort) {
    case "name":
      return byName
    case "uptime":
      return up(a) - up(b) || byStatus || byName
    case "latency":
      return lat(b) - lat(a) || byStatus || byName
    case "changed":
      return (b.status_since ?? 0) - (a.status_since ?? 0) || byName
    default:
      return byStatus || byName
  }
}

/** Dashboard filters, kept in the URL so they survive back and reload. */
function useFilters() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname() || "/"
  const q = params.get("q") ?? ""
  const st = params.get("status")
  const filter: Filter = filters.includes(st as Filter) ? (st as Filter) : "all"
  // "all", "none" (no group) or a group id
  const group = params.get("group") ?? "all"
  const tagParam = Number(params.get("tag"))
  const tag = tagParam > 0 ? tagParam : null
  const so = params.get("sort")
  const sort: Sort = sorts.includes(so as Sort) ? (so as Sort) : "status"

  const update = useCallback(
    (key: string, value: string | null) => {
      const next = new URLSearchParams(window.location.search)
      if (value === null || value === "") next.delete(key)
      else next.set(key, value)
      const qs = next.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [router, pathname]
  )
  return {
    q,
    filter,
    group,
    tag,
    sort,
    setQ: (v: string) => update("q", v),
    setFilter: (v: Filter) => update("status", v === "all" ? null : v),
    setGroup: (v: string) => update("group", v === "all" ? null : v),
    setTag: (v: number | null) => update("tag", v === null ? null : String(v)),
    setSort: (v: Sort) => update("sort", v === "status" ? null : v),
  }
}

export default function MonitorsPage() {
  return (
    <Suspense>
      <Monitors />
    </Suspense>
  )
}

function Monitors() {
  const { data, isLoading, mutate } = useApi<MonitorSummary[]>("/api/monitors")
  const isAdmin = useIsAdmin()
  const t = useTranslations("dashboard")
  const ts = useTranslations("status")
  const { q, filter, group, tag, sort, setQ, setFilter, setGroup, setTag, setSort } =
    useFilters()
  // The search box is local state so typing stays smooth; the URL follows.
  const [query, setQuery] = useState(q)
  const [lastQ, setLastQ] = useState(q)
  if (lastQ !== q) {
    setLastQ(q)
    setQuery(q)
  }
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const pendingDeletes = usePendingDeletes()
  const collapsed = useCollapsedGroups()
  const pins = usePins()
  const router = useRouter()
  const [busy, setBusy] = useState(false)
  const now = useMinute()
  useHotkey("/", () => document.getElementById("monitor-search")?.focus())
  const { groups, tags, groupById, tagById } = useLabels()
  const hasGroups = (groups.data?.length ?? 0) > 0

  const counts = useMemo(() => {
    const c: Record<string, number> = { all: data?.length ?? 0 }
    for (const m of data ?? []) c[m.status] = (c[m.status] ?? 0) + 1
    return c
  }, [data])

  const list = useMemo(() => {
    const s = query.toLowerCase()
    return (data ?? [])
      .filter((m) => !pendingDeletes.has(m.id))
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
      .sort((a, b) => compare(sort, a, b))
  }, [data, query, filter, group, tag, sort, groupById, tagById, pendingDeletes])

  // Selected monitors that still exist and are shown.
  const visibleSelected = list.filter((m) => selected.has(m.id))
  const allSelected = list.length > 0 && visibleSelected.length === list.length
  function toggle(id: number, on: boolean) {
    setSelected((p) => {
      const next = new Set(p)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  }
  function toggleAll(on: boolean) {
    setSelected(on ? new Set(list.map((m) => m.id)) : new Set())
  }
  async function bulk(
    action: "pause" | "resume" | "group",
    groupId = 0,
    ids = visibleSelected.map((m) => m.id),
    undoable = true
  ) {
    if (ids.length === 0) return
    setBusy(true)
    try {
      await api("/api/monitors/bulk", {
        method: "POST",
        body: { ids, action, group_id: groupId },
      })
      await mutate()
      if (!undoable) {
        toast(t("bulk.undone"))
        return
      }
      // Undo puts back the previous state of each monitor.
      const before = (data ?? []).filter((m) => ids.includes(m.id))
      const undo = () => {
        if (action === "group") {
          const byGroup = new Map<number, number[]>()
          for (const m of before) byGroup.set(m.group_id, [...(byGroup.get(m.group_id) ?? []), m.id])
          for (const [g, gIds] of byGroup) void bulk("group", g, gIds, false)
          return
        }
        const changed = before.filter((m) => m.paused !== (action === "pause")).map((m) => m.id)
        void bulk(action === "pause" ? "resume" : "pause", 0, changed, false)
      }
      toast.success(t(`bulk.done.${action}`, { count: ids.length }), {
        action: { label: t("bulk.undo"), onClick: undo },
      })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("bulk.error"))
    } finally {
      setBusy(false)
    }
  }

  function bulkDelete() {
    const ids = visibleSelected.map((m) => m.id)
    setSelected(new Set())
    deleteWithUndo(
      ids,
      {
        done: t("bulk.done.delete", { count: ids.length }),
        undo: t("bulk.undo"),
        undone: t("bulk.undone"),
        error: t("bulk.error"),
      },
      () => void mutate()
    )
  }


  // Pinned monitors come first, in their own section. With groups, the rest
  // is split in one section per group (by name), with monitors without a
  // group last.
  const sections = useMemo(() => {
    const pinned = list.filter((m) => pins.has(m.id))
    const rest = list.filter((m) => !pins.has(m.id))
    const top = pinned.length ? [{ key: "pinned", title: t("pinned"), items: pinned }] : []
    if (!hasGroups)
      return [...top, ...(rest.length ? [{ key: "all", title: top.length ? t("others") : "", items: rest }] : [])]
    const by = new Map<number, MonitorSummary[]>()
    for (const m of rest) {
      const k = groupById.has(m.group_id) ? m.group_id : 0
      by.set(k, [...(by.get(k) ?? []), m])
    }
    const out = (groups.data ?? [])
      .filter((g) => by.has(g.id))
      .map((g) => ({ key: String(g.id), title: g.name, items: by.get(g.id)! }))
    if (by.has(0))
      out.push({ key: "none", title: t("ungrouped"), items: by.get(0)! })
    return [...top, ...out]
  }, [list, pins, hasGroups, groups.data, groupById, t])

  return (
    <>
      <PageHeader cmd="pingtxt monitors ls" title={t("title")}>
        <Input
          id="monitor-search"
          placeholder={t("search")}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setQ(e.target.value)
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setQuery("")
              setQ("")
              e.currentTarget.blur()
            }
          }}
          className="w-40"
          title={t("searchHint")}
        />
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="outline" title={t("sort.title")}>
                {t("sort.label", { sort: t(`sort.${sort}`) })}
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="min-w-48">
            <DropdownMenuRadioGroup
              value={sort}
              onValueChange={(v) => setSort(v as Sort)}
            >
              {sorts.map((so) => (
                <DropdownMenuRadioItem key={so} value={so} closeOnClick>
                  {t(`sort.${so}`)}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        {isAdmin && (
          <Button render={<Link href="/monitors/new/" />} nativeButton={false}>
            <PlusIcon /> {t("new")}
          </Button>
        )}
      </PageHeader>

      <div className="mb-4 flex flex-wrap gap-1 text-xs">
        {filters.map((f) => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={segment({ active: filter === f })}
          >
            {f === "all" ? t("all") : ts(f)}:{counts[f] ?? 0}
          </button>
        ))}
      </div>

      {(hasGroups || (tags.data?.length ?? 0) > 0) && (
        <div className="-mt-2 mb-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
          {hasGroups && (
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-muted-foreground">{t("group")}</span>
              {[
                { k: "all", label: t("all") },
                ...(groups.data ?? []).map((g) => ({
                  k: String(g.id),
                  label: g.name,
                })),
                { k: "none", label: t("ungrouped") },
              ].map((g) => (
                <button
                  key={g.k}
                  onClick={() => setGroup(g.k)}
                  className={segment({ active: group === g.k })}
                >
                  {g.label}
                </button>
              ))}
            </div>
          )}
          {(tags.data?.length ?? 0) > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-muted-foreground">{t("tag")}</span>
              {(tags.data ?? []).map((tg) => (
                <TagToggle
                  key={tg.id}
                  tag={tg}
                  on={tag === tg.id}
                  onChange={(on) => setTag(on ? tg.id : null)}
                />
              ))}
            </div>
          )}
        </div>
      )}

      {isLoading && !data ? (
        <div className="text-muted-foreground">
          {t("fetching")}<span className="cursor-blink">_</span>
        </div>
      ) : list.length === 0 ? (
        <Empty>
          {data?.length ? (
            t("noMatch")
          ) : (
            <>
              {t.rich("emptyState", {
                link: (c) => (
                  <Link href="/monitors/new/" className="text-primary underline underline-offset-4">
                    {c}
                  </Link>
                ),
              })}
            </>
          )}
        </Empty>
      ) : (
        <div className="border-t border-dashed">
          <div className="flex items-center border-b border-dashed text-[11px] text-muted-foreground uppercase">
            {isAdmin && (
              <label className="flex items-center self-stretch py-1.5 pl-2" title={t("bulk.selectAll")}>
                <Checkbox
                  checked={allSelected}
                  onCheckedChange={(v) => toggleAll(v)}
                  aria-label={t("bulk.selectAll")}
                />
              </label>
            )}
            <div className="hidden flex-1 grid-cols-[1fr_220px_72px_64px_80px] gap-4 px-2 py-1.5 md:grid">
            <span>{t("cols.name")}</span>
            <span>{t("cols.last24h")}</span>
            <span className="text-right">{t("cols.uptime")}</span>
            <span className="text-right">{t("cols.avg")}</span>
            <span className="text-right">{t("cols.checked")}</span>
            </div>
            {isAdmin && (
              <span className="px-2 py-1.5 normal-case md:hidden">
                {t("bulk.selectAll")}
              </span>
            )}
            <span className="ml-auto w-8 shrink-0" />
          </div>
          {sections.map((sec) => (
            <div key={sec.key}>
              {sec.title && (
                <button
                  type="button"
                  onClick={() => collapsed.toggle(sec.key)}
                  aria-expanded={!collapsed.has(sec.key)}
                  className="flex w-full items-center gap-2 border-b border-dashed bg-muted/40 px-2 py-1 text-left text-xs hover:bg-muted/70"
                >
                  <span className="w-3 text-muted-foreground">{collapsed.has(sec.key) ? "▸" : "▾"}</span>
                  {sec.key === "pinned" && <span className="text-warn">★</span>}
                  <span className="font-medium">{sec.title}</span>
                  <span className="text-muted-foreground">
                    {sec.items.length}
                    {sec.items.some((m) => m.status === "down") && (
                      <span className="text-down">
                        {" · "}
                        {t("down", { count: sec.items.filter((m) => m.status === "down").length })}
                      </span>
                    )}
                  </span>
                </button>
              )}
              {!(sec.title && collapsed.has(sec.key)) && sec.items.map((m) => (
                <div
                  key={m.id}
                  className={cn(
                    "flex items-stretch border-b border-dashed transition-colors hover:bg-accent/60",
                    selected.has(m.id) && "bg-accent/40"
                  )}
                >
                {isAdmin && (
                  <label className="flex items-center pl-2">
                    <Checkbox
                      checked={selected.has(m.id)}
                      onCheckedChange={(v) => toggle(m.id, v)}
                      aria-label={t("bulk.select", { name: m.name })}
                    />
                  </label>
                )}
                <Link
                  href={`/monitors/view/?id=${m.id}`}
                  className="grid min-w-0 flex-1 grid-cols-[1fr_auto] items-center gap-x-4 gap-y-1 px-2 py-2.5 md:grid-cols-[1fr_220px_72px_64px_80px]"
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
                          ? t("heartbeatLine", { every: interval(m.interval_sec) })
                          : t("monitorLine", { type: m.type, target: displayTarget(m.target), every: interval(m.interval_sec) })}
                        {m.locations.length > 1 && ` · ${t("locations", { count: m.locations.length })}`}
                        <ExpiryBadges checks={m.expiry} now={now} />
                      </div>
                    </div>
                  </div>
                  <UptimeBars
                    bars={m.bars}
                    className="col-span-2 md:col-span-1"
                    onSelect={(from, to) =>
                      router.push(`/monitors/view/?id=${m.id}&from=${from}&to=${to}#log`)
                    }
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
                  <span
                    className="hidden text-right text-xs text-muted-foreground md:block"
                    title={m.last?.ts ? datetime(m.last.ts) : undefined}
                  >
                    {m.paused ? ts("paused") : ago(m.last?.ts)}
                  </span>
                </Link>
                <PinButton id={m.id} name={m.name} pinned={pins.has(m.id)} />
                </div>
              ))}
            </div>
          ))}
        </div>
      )}

      {isAdmin && visibleSelected.length > 0 && (
        <div className="sticky bottom-4 z-20 mt-4 flex flex-wrap items-center gap-2 border border-dashed bg-background/95 px-3 py-2 text-xs shadow-lg backdrop-blur">
          <span className="mr-auto">
            <span className="text-primary">$</span>{" "}
            {t("bulk.selected", { count: visibleSelected.length })}
          </span>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => bulk("pause")}>
            {t("bulk.pause")}
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => bulk("resume")}>
            {t("bulk.resume")}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button size="sm" variant="outline" disabled={busy}>
                  {t("bulk.moveToGroup")}
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="min-w-40">
              {(groups.data ?? []).map((g) => (
                <DropdownMenuItem key={g.id} onClick={() => bulk("group", g.id)}>
                  {g.name}
                </DropdownMenuItem>
              ))}
              {(groups.data?.length ?? 0) > 0 && <DropdownMenuSeparator />}
              <DropdownMenuItem onClick={() => bulk("group", 0)}>
                {t("bulk.noGroup")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button size="sm" variant="destructive" disabled={busy} onClick={bulkDelete}>
            {t("bulk.delete")}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>
            {t("bulk.clear")}
          </Button>
        </div>
      )}
    </>
  )
}

const collapsedKey = "pingtxt.collapsedGroups"

/** Group sections the user folded, remembered in this browser. */
function useCollapsedGroups() {
  const [keys, setKeys] = useState<Set<string>>(new Set())
  useEffect(() => {
    try {
      const v = JSON.parse(localStorage.getItem(collapsedKey) ?? "[]")
      // eslint-disable-next-line react-hooks/set-state-in-effect -- read once after mount (static export)
      if (Array.isArray(v)) setKeys(new Set(v.map(String)))
    } catch {}
  }, [])
  return {
    has: (k: string) => keys.has(k),
    toggle: (k: string) =>
      setKeys((prev) => {
        const next = new Set(prev)
        if (next.has(k)) next.delete(k)
        else next.add(k)
        try {
          localStorage.setItem(collapsedKey, JSON.stringify([...next]))
        } catch {}
        return next
      }),
  }
}
