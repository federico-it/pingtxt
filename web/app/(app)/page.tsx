"use client"

import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { Suspense, useCallback, useEffect, useMemo, useState } from "react"
import { LayoutGridIcon, ListIcon, PlusIcon, TvIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"


import { AgentsOffline } from "@/components/agents-offline"
import { OnboardingChecklist } from "@/components/onboarding"
import { MonitorCard, MonitorMarks, MonitorSubline, StatusDots, uptimeClass } from "@/components/dashboard-items"
import { MonitorIcon } from "@/components/monitor-icon"
import { TagChip, TagList, useLabels } from "@/components/labels"
import { Empty, PageHeader } from "@/components/page-header"
import { StatusGlyph, StatusText } from "@/components/status"
import { UptimeBars } from "@/components/uptime-bars"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
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
import { ago, datetime, displayTarget, ms, pct } from "@/lib/format"
import { deleteWithUndo, usePendingDeletes } from "@/lib/pending-delete"
import { usePins } from "@/lib/pins"
import { PinButton } from "@/components/pin-button"
import { segment } from "@/components/ui/control"
import { shortGroupName } from "@/lib/groups"
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
  const lat = (m: MonitorSummary) => (m.avg_ms_24h > 0 && m.type !== "server" ? m.avg_ms_24h : -1)
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

/** Rank of the worst monitor of a group for "sort: status": down, then
 * degraded; any other status leaves the group in its usual place. */
function problemRank(list: MonitorSummary[]) {
  let r = 2
  for (const m of list) if (m.status === "down") return 0
  else if (m.status === "degraded") r = 1
  return r
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
  // tag ids, comma separated: monitors with any of them
  const tagParam = params.get("tag") ?? ""
  const tags = useMemo(
    () => [...new Set(tagParam.split(",").map(Number).filter((n) => Number.isInteger(n) && n > 0))],
    [tagParam]
  )
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
    tags,
    sort,
    setQ: (v: string) => update("q", v),
    setFilter: (v: Filter) => update("status", v === "all" ? null : v),
    setGroup: (v: string) => update("group", v === "all" ? null : v),
    setTags: (v: number[]) => update("tag", v.length ? v.join(",") : null),
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
  const { q, filter, group, tags: tagFilter, sort, setQ, setFilter, setGroup, setTags, setSort } =
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
  const { groups, tags, groupById, tagById, tree } = useLabels()
  const hasGroups = (groups.data?.length ?? 0) > 0

  // Monitors per group filter: a group counts its subgroups' monitors too.
  const groupCounts = useMemo(() => {
    const c = new Map<string, number>()
    for (const m of data ?? []) {
      if (pendingDeletes.has(m.id)) continue
      const g = groupById.get(m.group_id)
      const keys = g ? [g.id, ...g.ancestors].map(String) : ["none"]
      for (const k of keys) c.set(k, (c.get(k) ?? 0) + 1)
    }
    return c
  }, [data, groupById, pendingDeletes])
  // Monitors per tag, for the tag filter.
  const tagCounts = useMemo(() => {
    const c = new Map<number, number>()
    for (const m of data ?? []) {
      if (pendingDeletes.has(m.id)) continue
      for (const id of m.tag_ids ?? []) c.set(id, (c.get(id) ?? 0) + 1)
    }
    return c
  }, [data, pendingDeletes])
  const selectedTags = tagFilter.map((id) => tagById.get(id)).filter((tg) => !!tg)
  const groupLabel =
    group === "all"
      ? t("all")
      : group === "none"
        ? t("ungrouped")
        : (groupById.get(Number(group))?.path ?? t("all"))

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
            : String(m.group_id) === group ||
              !!groupById.get(m.group_id)?.ancestors.includes(Number(group)))
      )
      .filter((m) => tagFilter.length === 0 || !!m.tag_ids?.some((id) => tagFilter.includes(id)))
      .filter(
        (m) =>
          !s ||
          m.name.toLowerCase().includes(s) ||
          displayTarget(m.target).toLowerCase().includes(s) ||
          m.type.includes(s) ||
          m.tag_ids?.some((id) =>
            tagById.get(id)?.name.toLowerCase().includes(s)
          ) ||
          groupById.get(m.group_id)?.path.toLowerCase().includes(s)
      )
      .sort((a, b) => compare(sort, a, b))
  }, [data, query, filter, group, tagFilter, sort, groupById, tagById, pendingDeletes])

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
  // is split in one block per top-level group, with monitors without a group
  // last. Subgroups of any depth are flat parts of their top-level group's
  // block, named by their path below it ("DNS › Resolvers"), so the list
  // never steps further right than one level.
  const blocks = useMemo((): Block[] => {
    const pinned = list.filter((m) => pins.has(m.id))
    const rest = list.filter((m) => !pins.has(m.id))
    const top: Block[] = pinned.length
      ? [{ key: "pinned", title: t("pinned"), items: pinned, all: pinned, subs: [] }]
      : []
    if (!hasGroups)
      return [
        ...top,
        ...(rest.length ? [{ key: "all", title: top.length ? t("others") : "", items: rest, all: rest, subs: [] }] : []),
      ]
    const by = new Map<number, MonitorSummary[]>()
    for (const m of rest) {
      const k = groupById.has(m.group_id) ? m.group_id : 0
      by.set(k, [...(by.get(k) ?? []), m])
    }
    let groupsWithMonitors = tree.ordered
      .map((g) => ({
        g,
        items: by.get(g.id) ?? [],
        // every monitor of the group and its subgroups, for the summary
        nested: [g.id, ...tree.descendants(g.id)].flatMap((id) => by.get(id) ?? []),
      }))
      .filter((x) => x.nested.length > 0)
    // Sorted by status, groups with monitors down (then degraded) come
    // first among their siblings; the others keep their order.
    if (sort === "status") {
      const kids = new Map<number, typeof groupsWithMonitors>()
      for (const x of groupsWithMonitors) {
        const p = x.g.ancestors[0] ?? 0
        kids.set(p, [...(kids.get(p) ?? []), x])
      }
      const sorted: typeof groupsWithMonitors = []
      const walk = (p: number) => {
        const sib = (kids.get(p) ?? [])
          .map((x, i) => ({ x, i, r: problemRank(x.nested) }))
          .sort((a, b) => a.r - b.r || a.i - b.i)
        for (const { x } of sib) {
          sorted.push(x)
          walk(x.g.id)
        }
      }
      walk(0)
      groupsWithMonitors = sorted
    }
    const out: Block[] = []
    for (const { g, items, nested } of groupsWithMonitors) {
      if (g.depth === 0 || out.length === 0) {
        out.push({ key: String(g.id), title: shortGroupName(g, groupById), fullTitle: g.path, items, all: nested, subs: [], group: true })
        continue
      }
      // A subgroup with no monitors of its own needs no heading: its
      // subgroups' headings carry its name.
      if (items.length === 0) continue
      const chain = [...g.ancestors].reverse().slice(1).map((id) => groupById.get(id)).filter((x) => !!x)
      out[out.length - 1].subs.push({
        key: String(g.id),
        title: [...chain, g].map((n) => shortGroupName(n, groupById)).join(" › "),
        fullTitle: g.path,
        items,
        all: nested,
        ancestors: g.ancestors,
      })
    }
    if (by.has(0)) out.push({ key: "none", title: t("ungrouped"), items: by.get(0)!, all: by.get(0)!, subs: [] })
    return [...top, ...out]
  }, [list, pins, hasGroups, tree, groupById, sort, t])
  // Headings that can be folded, for "fold all".
  const foldable = blocks.flatMap((b) => (b.group ? [b.key, ...b.subs.map((sub) => sub.key)] : []))
  const allFolded = foldable.length > 0 && foldable.every((k) => collapsed.has(k))
  const view = useDashboardView()
  const selecting = visibleSelected.length > 0

  // Cards: one grid per block, so a subgroup with one or two monitors does
  // not take a row of its own; each card names its subgroup.
  function renderCards(blk: Block) {
    const cards = [
      ...blk.items.map((m) => ({ m, sub: "", subFull: "" })),
      ...blk.subs.flatMap((sub) => sub.items.map((m) => ({ m, sub: sub.title, subFull: sub.fullTitle }))),
    ]
    return (
      <div className="grid gap-3 py-3 sm:grid-cols-2 lg:grid-cols-3">
        {cards.map(({ m, sub, subFull }) => (
          <MonitorCard
            key={m.id}
            m={m}
            group={sub}
            groupTitle={subFull}
            now={now}
            tagById={tagById}
            pinned={pins.has(m.id)}
            selected={selected.has(m.id)}
            selecting={selecting}
            onSelect={isAdmin ? (on) => toggle(m.id, on) : undefined}
          />
        ))}
      </div>
    )
  }

  function renderItems(items: MonitorSummary[]) {
    if (view.value === "cards")
      return (
        <div className="grid gap-3 py-3 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((m) => (
            <MonitorCard
              key={m.id}
              m={m}
              now={now}
              tagById={tagById}
              pinned={pins.has(m.id)}
              selected={selected.has(m.id)}
              selecting={selecting}
              onSelect={isAdmin ? (on) => toggle(m.id, on) : undefined}
            />
          ))}
        </div>
      )
    return items.map((m) => (
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
            <MonitorIcon m={m} className="size-6" />
            <div className="min-w-0">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate font-medium">{m.name}</span>
                <MonitorMarks m={m} />
                <TagList ids={m.tag_ids} tagById={tagById} className="hidden shrink-0 sm:inline-flex" />
              </div>
              <MonitorSubline m={m} now={now} />
            </div>
          </div>
          <UptimeBars
            bars={m.bars}
            className="col-span-2 md:col-span-1"
            onSelect={(from, to) => router.push(`/monitors/view/?id=${m.id}&from=${from}&to=${to}#log`)}
          />
          <span className={cn("text-right", uptimeClass(m.uptime_24h))}>
            <span className="md:hidden">
              <StatusText status={m.status} className="mr-2" />
            </span>
            {pct(m.uptime_24h)}
          </span>
          <span className="hidden text-right text-muted-foreground md:block">
            {/* a server check's run time is no response time */}
            {m.type === "server" ? "" : ms(m.avg_ms_24h)}
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
    ))
  }

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
        <Button
          variant="outline"
          render={<Link href={tvHref(group, tagFilter)} />}
          nativeButton={false}
          title={t("tvHint")}
          aria-label={t("tvHint")}
        >
          <TvIcon /> <span className="hidden sm:inline">{t("tv")}</span>
        </Button>
        {isAdmin && (
          <Button variant="outline" render={<Link href="/monitors/presets/" />} nativeButton={false}>
            <LayoutGridIcon /> {t("services")}
          </Button>
        )}
        {isAdmin && (
          <Button render={<Link href="/monitors/new/" />} nativeButton={false}>
            <PlusIcon /> {t("new")}
          </Button>
        )}
      </PageHeader>

      <AgentsOffline />
      <OnboardingChecklist enabled={!!isAdmin} />

      <div className="mb-4 flex flex-wrap items-center gap-1 text-xs">
        {filters.map((f) => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={segment({ active: filter === f })}
          >
            {f === "all" ? t("all") : ts(f)}:{counts[f] ?? 0}
          </button>
        ))}
        <div className="ml-auto flex items-center gap-px" role="group" aria-label={t("view.label")}>
          {(["list", "cards"] as const).map((v) => {
            const Icon = v === "list" ? ListIcon : LayoutGridIcon
            return (
              <button
                key={v}
                type="button"
                onClick={() => view.set(v)}
                aria-pressed={view.value === v}
                title={t(`view.${v}`)}
                className={cn(segment({ active: view.value === v }), "gap-1.5")}
              >
                <Icon className="size-3.5" />
                <span className="hidden sm:inline">{t(`view.${v}`)}</span>
              </button>
            )
          })}
        </div>
      </div>

      {(hasGroups || (tags.data?.length ?? 0) > 0) && (
        <div className="-mt-2 mb-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
          {hasGroups && (
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-muted-foreground">{t("group")}</span>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      className={cn(segment({ active: group !== "all" }), "max-w-72 gap-1")}
                      title={t("groupFilter")}
                    >
                      <span className="truncate">{groupLabel}</span>
                      <span aria-hidden className="text-muted-foreground">▾</span>
                    </button>
                  }
                />
                <DropdownMenuContent align="start" className="w-auto min-w-56 max-w-[min(24rem,calc(100vw-2rem))]">
                  <DropdownMenuRadioGroup value={group} onValueChange={(v) => setGroup(String(v))}>
                    <DropdownMenuRadioItem value="all" closeOnClick>
                      <span className="flex-1">{t("all")}</span>
                      <span className="text-xs text-muted-foreground">{counts.all}</span>
                    </DropdownMenuRadioItem>
                    <DropdownMenuSeparator />
                    {tree.ordered.map((g) => (
                      <DropdownMenuRadioItem
                        key={g.id}
                        value={String(g.id)}
                        closeOnClick
                        style={{ paddingLeft: `${0.375 + g.depth * 1.125}rem` }}
                      >
                        {g.depth > 0 && <span aria-hidden className="text-muted-foreground">└</span>}
                        <span className={cn("flex-1 truncate", g.depth === 0 && "font-medium")} title={g.path}>{shortGroupName(g, groupById)}</span>
                        <span className="text-xs text-muted-foreground">{groupCounts.get(String(g.id)) ?? 0}</span>
                      </DropdownMenuRadioItem>
                    ))}
                    <DropdownMenuSeparator />
                    <DropdownMenuRadioItem value="none" closeOnClick>
                      <span className="flex-1">{t("ungrouped")}</span>
                      <span className="text-xs text-muted-foreground">{groupCounts.get("none") ?? 0}</span>
                    </DropdownMenuRadioItem>
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
              {group !== "all" && (
                <button type="button" onClick={() => setGroup("all")} className={segment()} title={t("clearGroup")}>
                  ×
                </button>
              )}
              {foldable.length > 0 && (
                <button
                  type="button"
                  onClick={() => collapsed.set(allFolded ? [] : foldable)}
                  className={segment()}
                >
                  {allFolded ? t("unfoldAll") : t("foldAll")}
                </button>
              )}
            </div>
          )}
          {(tags.data?.length ?? 0) > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-muted-foreground">{t("tag")}</span>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      className={cn(segment({ active: selectedTags.length > 0 }), "max-w-72 gap-1")}
                      title={t("tagFilter")}
                    >
                      {selectedTags.length === 0 ? (
                        <span>{t("all")}</span>
                      ) : (
                        <span className="flex min-w-0 items-center gap-1">
                          <TagChip tag={selectedTags[0]} className="min-w-0 truncate" />
                          {selectedTags.length > 1 && <span className="shrink-0">+{selectedTags.length - 1}</span>}
                        </span>
                      )}
                      <span aria-hidden className="text-muted-foreground">▾</span>
                    </button>
                  }
                />
                <DropdownMenuContent align="start" className="w-auto min-w-48 max-w-[min(24rem,calc(100vw-2rem))]">
                  {(tags.data ?? []).map((tg) => (
                    <DropdownMenuCheckboxItem
                      key={tg.id}
                      checked={tagFilter.includes(tg.id)}
                      onCheckedChange={(on) =>
                        setTags(on ? [...tagFilter, tg.id] : tagFilter.filter((id) => id !== tg.id))
                      }
                    >
                      <span className="flex-1">
                        <TagChip tag={tg} />
                      </span>
                      <span className="text-xs text-muted-foreground">{tagCounts.get(tg.id) ?? 0}</span>
                    </DropdownMenuCheckboxItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
              {selectedTags.length > 0 && (
                <button type="button" onClick={() => setTags([])} className={segment()} title={t("clearTags")}>
                  ×
                </button>
              )}
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
                services: (c) => (
                  <Link href="/monitors/presets/" className="text-primary underline underline-offset-4">
                    {c}
                  </Link>
                ),
              })}
            </>
          )}
        </Empty>
      ) : (
        <div className={cn(view.value === "list" && "border-t border-dashed")}>
          {view.value === "list" && (
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
              {isAdmin && <span className="px-2 py-1.5 normal-case md:hidden">{t("bulk.selectAll")}</span>}
              <span className="ml-auto w-11 shrink-0" />
            </div>
          )}
          {view.value === "cards" && isAdmin && (
            <label className="mb-1 inline-flex items-center gap-2 py-1 text-xs text-muted-foreground">
              <Checkbox checked={allSelected} onCheckedChange={(v) => toggleAll(v)} />
              {t("bulk.selectAll")}
            </label>
          )}
          {blocks.map((blk, i) => {
            const folded = !!blk.title && collapsed.has(blk.key)
            // Subgroups under a folded subgroup hide with it.
            const shown = new Set(blk.subs.map((sub) => sub.key))
            const subs = blk.subs.filter(
              (sub) => !sub.ancestors.some((a) => shown.has(String(a)) && collapsed.has(String(a)))
            )
            return (
              <section key={blk.key} className={cn(i > 0 && blk.title && "mt-6")}>
                {blk.title && (
                  <GroupHeading
                    level={0}
                    title={blk.title}
                    fullTitle={blk.fullTitle}
                    pinned={blk.key === "pinned"}
                    list={blk.all}
                    folded={folded}
                    onToggle={() => collapsed.toggle(blk.key)}
                  />
                )}
                {!folded && view.value === "cards" && renderCards(blk)}
                {!folded && view.value === "list" && (
                  <>
                    {renderItems(blk.items)}
                    {subs.map((sub) => {
                      const subFolded = collapsed.has(sub.key)
                      return (
                        <div key={sub.key} className="pl-4 sm:pl-6">
                          <GroupHeading
                            level={1}
                            title={sub.title}
                            fullTitle={sub.fullTitle}
                            list={sub.items}
                            folded={subFolded}
                            onToggle={() => collapsed.toggle(sub.key)}
                          />
                          {!subFolded && renderItems(sub.items)}
                        </div>
                      )
                    })}
                  </>
                )}
              </section>
            )
          })}
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
              {tree.ordered.map((g) => (
                <DropdownMenuItem key={g.id} onClick={() => bulk("group", g.id)}>
                  {g.path}
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

/** TV mode with the dashboard's group and tag filter. */
function tvHref(group: string, tags: number[]) {
  const p = new URLSearchParams()
  if (group !== "all") p.set("group", group)
  if (tags.length) p.set("tag", tags.join(","))
  return p.size ? `/tv/?${p}` : "/tv/"
}

/** A block of the monitor list: pinned, a top-level group or the ungrouped ones. */
type Block = {
  key: string
  title: string
  /** monitors right in it */
  items: MonitorSummary[]
  /** its monitors and those of its subgroups, for the summary */
  all: MonitorSummary[]
  /** subgroups of any depth with monitors of their own, in tree order */
  subs: Sub[]
  /** a group (folded with "fold all") */
  group?: boolean
  /** path from the top-level group, when title is shortened */
  fullTitle?: string
}

type Sub = {
  key: string
  /** path below the top-level group, short names */
  title: string
  fullTitle: string
  items: MonitorSummary[]
  all: MonitorSummary[]
  /** groups it is inside of, parent first */
  ancestors: number[]
}

/** Heading of a group, the pinned or the ungrouped monitors: folds them
 * and sums up their status. */
function GroupHeading({
  level,
  title,
  fullTitle,
  pinned,
  list,
  folded,
  onToggle,
}: {
  level: 0 | 1
  title: string
  fullTitle?: string
  pinned?: boolean
  list: MonitorSummary[]
  folded: boolean
  onToggle: () => void
}) {
  const t = useTranslations("dashboard")
  const down = list.filter((m) => m.status === "down").length
  const degraded = list.filter((m) => m.status === "degraded").length
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={!folded}
      className={cn(
        "group/heading flex w-full min-w-0 items-center gap-2 border-b text-left transition-colors",
        level === 0
          ? "border-foreground/25 py-2 text-[13px] hover:bg-muted/50"
          : "border-dashed py-1.5 text-xs text-muted-foreground hover:bg-muted/40 hover:text-foreground"
      )}
    >
      <span className="w-4 shrink-0 pl-1 text-muted-foreground">{folded ? "▸" : "▾"}</span>
      {pinned && <span className="text-warn">★</span>}
      <span
        className={cn("truncate", level === 0 ? "font-semibold" : "font-medium")}
        title={fullTitle && fullTitle !== title ? fullTitle : undefined}
      >
        {title}
      </span>
      <span className="shrink-0 text-xs text-muted-foreground">
        {list.length}
        {down > 0 && <span className="text-down"> · {t("down", { count: down })}</span>}
        {degraded > 0 && <span className="text-warn"> · {t("degraded", { count: degraded })}</span>}
      </span>
      <StatusDots list={list} className="ml-auto pr-2" max={level === 0 ? 24 : 16} />
    </button>
  )
}

const viewKey = "pingtxt.dashboardView"
type View = "list" | "cards"

/** List or cards, remembered in this browser. */
function useDashboardView() {
  const [value, setValue] = useState<View>("list")
  useEffect(() => {
    try {
      const v = localStorage.getItem(viewKey)
      // eslint-disable-next-line react-hooks/set-state-in-effect -- read once after mount (static export)
      if (v === "cards" || v === "list") setValue(v)
    } catch {}
  }, [])
  return {
    value,
    set: (v: View) => {
      setValue(v)
      try {
        localStorage.setItem(viewKey, v)
      } catch {}
    },
  }
}

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
    set: (list: string[]) => {
      setKeys(new Set(list))
      try {
        localStorage.setItem(collapsedKey, JSON.stringify(list))
      } catch {}
    },
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
