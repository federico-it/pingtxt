"use client"

import Link from "next/link"
import { useSearchParams } from "next/navigation"
import {
  Suspense,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react"
import { MaximizeIcon, MinimizeIcon, UserCheckIcon, XIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { useReasonLine } from "@/components/reasons"
import { StatusGlyph } from "@/components/status"
import { useLabels } from "@/components/labels"
import { useAckName } from "@/components/incident-ack"
import { useNow } from "@/hooks/use-now"
import {
  useApi,
  type Me,
  type MonitorSummary,
  type Overview,
  type Status,
} from "@/lib/api"
import { duration, pct } from "@/lib/format"
import { shortGroupPath } from "@/lib/groups"
import { cn } from "@/lib/utils"

// TV mode: the dashboard for a wall screen. No menus, large text, problems
// first, the rest as a grid of tiles. It refreshes on its own, keeps the
// screen awake and hides the mouse pointer when it does not move.
//
// Nothing scrolls: the tiles flow in columns across the whole screen, as
// large as they can be while everything fits. When even the smallest size
// does not fit, the columns are split into pages that rotate on their own.

/** Tile sizes, largest first. */
const SIZES = [
  "2.25rem",
  "2rem",
  "1.75rem",
  "1.5rem",
  "1.3125rem",
  "1.125rem",
  "1rem",
  "0.875rem",
  "0.75rem",
]
/** Seconds each page stays on screen when there is more than one. */
const PAGE_SECONDS = 15

const problemOrder: Partial<Record<Status, number>> = {
  down: 0,
  degraded: 1,
  maintenance: 2,
}

const tile: Record<Status, string> = {
  up: "border-up/40 bg-up/5",
  down: "border-down bg-down/15",
  degraded: "border-warn bg-warn/10",
  maintenance: "border-info/60 bg-info/10",
  paused: "border-border bg-muted/30 text-muted-foreground",
  pending: "border-border bg-muted/30 text-muted-foreground",
}

export default function TVPage() {
  return (
    <Suspense>
      <TV />
    </Suspense>
  )
}

function TV() {
  const t = useTranslations("tv")
  const ts = useTranslations("status")
  const tc = useTranslations("common")
  const params = useSearchParams()
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 60_000 })
  const { data } = useApi<MonitorSummary[]>(me.data ? "/api/monitors" : null)
  const ov = useApi<Overview>(me.data ? "/api/overview" : null)
  const { groupById, tagById } = useLabels()
  const reasonLine = useReasonLine()
  const ackName = useAckName()
  const now = useNow()

  // Same filters as the dashboard, so its "tv" button keeps them.
  const group = params.get("group") ?? "all"
  // tag ids, comma separated: monitors with any of them
  const tagParam = params.get("tag") ?? ""
  const tags = useMemo(
    () => [
      ...new Set(
        tagParam
          .split(",")
          .map(Number)
          .filter((n) => Number.isInteger(n) && n > 0)
      ),
    ],
    [tagParam]
  )
  const showPaused = params.get("paused") === "1"

  const monitors = useMemo(
    () =>
      (data ?? [])
        .filter((m) => showPaused || !m.paused)
        .filter(
          (m) =>
            group === "all" ||
            (group === "none"
              ? !groupById.has(m.group_id)
              : String(m.group_id) === group ||
                !!groupById.get(m.group_id)?.ancestors.includes(Number(group)))
        )
        .filter(
          (m) =>
            tags.length === 0 || !!m.tag_ids?.some((id) => tags.includes(id))
        ),
    [data, group, tags, groupById, showPaused]
  )
  const problems = monitors
    .filter((m) => problemOrder[m.status] !== undefined)
    .sort(
      (a, b) =>
        problemOrder[a.status]! - problemOrder[b.status]! ||
        (a.status_since ?? 0) - (b.status_since ?? 0) ||
        a.name.localeCompare(b.name)
    )
  const rest = monitors
    .filter((m) => problemOrder[m.status] === undefined)
    .sort((a, b) => {
      const ga = groupById.get(a.group_id)?.path ?? "￿"
      const gb = groupById.get(b.group_id)?.path ?? "￿"
      return ga.localeCompare(gb) || a.name.localeCompare(b.name)
    })
  // The rest by group, in the groups' order, monitors without a group last.
  const sections: { key: string; title: string; items: MonitorSummary[] }[] = []
  for (const m of rest) {
    const g = groupById.get(m.group_id)
    const key = g ? String(g.id) : "none"
    const last = sections[sections.length - 1]
    if (last?.key === key) last.items.push(m)
    else
      sections.push({
        key,
        title: g ? shortGroupPath(g, groupById) : t("ungrouped"),
        items: [m],
      })
  }
  const count = (s: Status) => monitors.filter((m) => m.status === s).length
  const down = count("down")
  const degraded = count("degraded")
  const overall: Status = down > 0 ? "down" : degraded > 0 ? "degraded" : "up"

  const scopeLabel = [
    group === "all"
      ? ""
      : group === "none"
        ? t("ungrouped")
        : groupById.get(Number(group))
          ? shortGroupPath(groupById.get(Number(group))!, groupById)
          : "",
    tags
      .map((id) => tagById.get(id)?.name)
      .filter(Boolean)
      .join(", "),
  ]
    .filter(Boolean)
    .join(" · ")

  useWakeLock()
  const idle = useIdlePointer()
  const [full, setFull] = useState(false)
  useEffect(() => {
    const on = () => setFull(!!document.fullscreenElement)
    document.addEventListener("fullscreenchange", on)
    return () => document.removeEventListener("fullscreenchange", on)
  }, [])
  function toggleFull() {
    if (document.fullscreenElement) void document.exitFullscreen()
    else void document.documentElement.requestFullscreen?.().catch(() => {})
  }

  // Tab title like the dashboard, so a glance at the tab bar is enough too.
  useEffect(() => {
    document.title = down > 0 ? `(${down}) pingtxt tv` : "pingtxt tv"
  }, [down])

  const many = problems.length > 6
  const layoutKey = [
    problems.length,
    many,
    sections.length > 1,
    ...sections.map(
      (sec) =>
        `${sec.title}:${sec.items.map((m) => `${m.id}/${m.name}`).join(",")}`
    ),
  ].join("|")
  const { outer, inner, level, pages } = useFit(layoutKey)
  const [tick, setTick] = useState(0)
  useEffect(() => {
    if (pages <= 1) return
    const id = setInterval(() => setTick((n) => n + 1), PAGE_SECONDS * 1000)
    return () => clearInterval(id)
  }, [pages])
  const page = pages > 1 ? tick % pages : 0

  if (!me.data || !data) {
    return (
      <div className="flex min-h-svh items-center justify-center text-muted-foreground">
        <span>
          {tc("loading")}
          <span className="cursor-blink">_</span>
        </span>
      </div>
    )
  }

  const back = new URLSearchParams()
  if (group !== "all") back.set("group", group)
  if (tags.length) back.set("tag", tags.join(","))
  const backHref = back.size ? `/?${back}` : "/"
  const clock = new Date(now)

  return (
    <div
      className={cn(
        "flex h-svh flex-col gap-4 overflow-hidden p-4 sm:p-6 lg:p-8",
        idle && "cursor-none"
      )}
    >
      <header className="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-2">
        <div className="text-lg font-semibold tracking-tight">
          <span className="text-primary">&gt;</span> pingtxt{" "}
          <span className="text-muted-foreground">tv</span>
          {scopeLabel && (
            <span className="ml-3 font-normal text-muted-foreground">
              {scopeLabel}
            </span>
          )}
        </div>
        <div className="ml-auto flex items-center gap-4 text-muted-foreground">
          {ov.data && ov.data.agents > 0 && (
            <span
              className={cn(
                ov.data.agents_online < ov.data.agents && "text-warn"
              )}
            >
              {t("agents", {
                online: ov.data.agents_online,
                total: ov.data.agents,
              })}
            </span>
          )}
          <span
            className="text-2xl text-foreground tabular-nums lg:text-3xl"
            suppressHydrationWarning
          >
            {now
              ? clock.toLocaleTimeString([], {
                  hour: "2-digit",
                  minute: "2-digit",
                })
              : ""}
          </span>
          <div
            className={cn(
              "flex items-center gap-1 transition-opacity",
              idle && "opacity-0"
            )}
          >
            <button
              type="button"
              onClick={toggleFull}
              className="rounded p-2 hover:bg-accent"
              title={full ? t("exitFullscreen") : t("fullscreen")}
              aria-label={full ? t("exitFullscreen") : t("fullscreen")}
            >
              {full ? (
                <MinimizeIcon className="size-5" />
              ) : (
                <MaximizeIcon className="size-5" />
              )}
            </button>
            <Link
              href={backHref}
              className="rounded p-2 hover:bg-accent"
              title={t("exit")}
              aria-label={t("exit")}
            >
              <XIcon className="size-5" />
            </Link>
          </div>
        </div>
      </header>

      <section
        className={cn(
          "flex shrink-0 flex-wrap items-end gap-x-8 gap-y-2 border px-5 py-4",
          tile[overall]
        )}
      >
        <div
          className={cn(
            "flex items-center gap-3 text-3xl font-semibold sm:text-4xl lg:text-5xl",
            overall === "down"
              ? "text-down"
              : overall === "degraded"
                ? "text-warn"
                : "text-up"
          )}
        >
          <StatusGlyph status={overall} />
          {down > 0
            ? t("down", { count: down })
            : degraded > 0
              ? t("degraded", { count: degraded })
              : t("allUp")}
        </div>
        <div className="flex flex-wrap gap-x-5 gap-y-1 text-base text-muted-foreground lg:text-lg">
          {(
            ["up", "down", "degraded", "maintenance", "pending"] as Status[]
          ).map((s) => {
            const n = count(s)
            if (n === 0 && s !== "up") return null
            return (
              <span key={s} className="whitespace-nowrap">
                <StatusGlyph status={s} /> {ts(s)}{" "}
                <span className="text-foreground tabular-nums">{n}</span>
              </span>
            )
          })}
        </div>
      </section>

      {monitors.length === 0 && (
        <div className="flex flex-1 items-center justify-center text-xl text-muted-foreground">
          {t("empty")}
        </div>
      )}

      {problems.length > 0 && (
        <section
          className={cn(
            "grid shrink-0 gap-3",
            many
              ? "[grid-template-columns:repeat(auto-fill,minmax(min(100%,18rem),1fr))]"
              : "[grid-template-columns:repeat(auto-fill,minmax(min(100%,24rem),1fr))]"
          )}
        >
          {problems.map((m) => (
            <div
              key={m.id}
              className={cn(
                "flex min-w-0 flex-col gap-1 border-2",
                many ? "px-3 py-2" : "px-4 py-3",
                tile[m.status]
              )}
            >
              <div
                className={cn(
                  "flex min-w-0 items-center gap-2 font-semibold",
                  many ? "text-lg" : "text-xl lg:text-2xl"
                )}
              >
                <StatusGlyph status={m.status} />
                <span className="truncate">{m.name}</span>
                <span
                  className={cn(
                    "ml-auto shrink-0 text-base font-normal tabular-nums",
                    m.status === "down"
                      ? "text-down"
                      : m.status === "degraded"
                        ? "text-warn"
                        : "text-info"
                  )}
                >
                  {m.status_since
                    ? duration(now - m.status_since)
                    : ts(m.status)}
                </span>
              </div>
              <div className="truncate text-sm text-muted-foreground lg:text-base">
                {m.status === "maintenance"
                  ? (m.maintenance?.name ?? ts("maintenance"))
                  : m.blocked_by
                    ? t("blockedBy", { name: m.blocked_by.name })
                    : (m.reasons?.length ?? 0) > 0
                      ? reasonLine(m.reasons!)
                      : m.last && !m.last.ok && m.last.message
                        ? m.last.message
                        : ts(m.status)}
              </div>
              {!many && (
                <div className="flex min-w-0 items-center gap-3 text-xs text-muted-foreground lg:text-sm">
                  {groupById.get(m.group_id) && (
                    <span className="truncate">
                      {shortGroupPath(groupById.get(m.group_id)!, groupById)}
                    </span>
                  )}
                  {!!m.incident?.acked_at && (
                    <span className="inline-flex shrink-0 items-center gap-1 text-info">
                      <UserCheckIcon className="size-3.5" />
                      {t("acked", { name: ackName(m.incident.acked_by) })}
                    </span>
                  )}
                </div>
              )}
            </div>
          ))}
        </section>
      )}

      {rest.length > 0 && (
        <div className="flex min-h-0 flex-1 flex-col gap-2">
          <div ref={outer} className="min-h-0 flex-1 overflow-hidden">
            <div
              ref={inner}
              className="h-full [column-gap:1.5em] transition-transform duration-700 ease-in-out [column-width:15em]"
              // one page: spread over every column; more: fill each column in turn
              style={{
                fontSize: SIZES[level],
                columnFill: pages > 1 ? "auto" : "balance",
                transform: `translateX(calc(${-page} * (100% + 1.5em)))`,
              }}
            >
              {sections.map((sec, i) => (
                <section key={sec.key} className="contents">
                  {sections.length > 1 && (
                    <h2
                      className={cn(
                        "mb-[0.6em] break-after-avoid truncate text-[max(0.6em,0.625rem)] tracking-wide text-muted-foreground uppercase",
                        i > 0 && "mt-[1.4em]"
                      )}
                    >
                      {sec.title}
                    </h2>
                  )}
                  {sec.items.map((m) => (
                    <div
                      key={m.id}
                      className={cn(
                        "mb-[0.35em] flex min-w-0 break-inside-avoid items-center gap-[0.5em] border px-[0.6em] py-[0.35em]",
                        tile[m.status]
                      )}
                    >
                      <StatusGlyph status={m.status} />
                      <span className="truncate">{m.name}</span>
                      <span
                        className={cn(
                          "ml-auto shrink-0 text-[0.8em] text-muted-foreground tabular-nums",
                          m.uptime_24h >= 0 &&
                            m.uptime_24h < 0.99 &&
                            "text-warn",
                          m.uptime_24h >= 0 && m.uptime_24h < 0.9 && "text-down"
                        )}
                      >
                        {pct(m.uptime_24h, 1)}
                      </span>
                    </div>
                  ))}
                </section>
              ))}
            </div>
          </div>
          {pages > 1 && (
            <div className="flex shrink-0 items-center justify-end gap-1.5 text-xs text-muted-foreground tabular-nums">
              {Array.from({ length: pages }, (_, i) => (
                <span
                  key={i}
                  className={cn(
                    "size-2 rounded-full",
                    i === page ? "bg-foreground" : "bg-muted-foreground/30"
                  )}
                />
              ))}
              <span className="ml-1">
                {page + 1}/{pages}
              </span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/** Picks the largest tile size at which the columns fit the box, and how many
 * pages they need at the smallest size when none does. Measured again when
 * the content (layoutKey) or the box size changes. */
function useFit(layoutKey: string) {
  const outer = useRef<HTMLDivElement>(null)
  const inner = useRef<HTMLDivElement>(null)
  const [fit, setFit] = useState({ level: 0, pages: 1 })
  useLayoutEffect(() => {
    const box = outer.current
    const el = inner.current
    if (!box || !el) return
    const measure = () => {
      const transform = el.style.transform
      const fontSize = el.style.fontSize
      const columnFill = el.style.columnFill
      el.style.transform = "none"
      el.style.columnFill = "auto"
      let level = SIZES.length - 1
      for (let i = 0; i < SIZES.length; i++) {
        el.style.fontSize = SIZES[i]
        if (el.scrollWidth <= el.clientWidth + 1) {
          level = i
          break
        }
      }
      el.style.fontSize = SIZES[level]
      // Columns that do not fit continue to the right, one page every
      // width + gap.
      const gap = parseFloat(getComputedStyle(el).columnGap) || 0
      const pages = Math.max(
        1,
        Math.ceil((el.scrollWidth + gap) / (el.clientWidth + gap) - 0.01)
      )
      el.style.transform = transform
      el.style.fontSize = fontSize
      el.style.columnFill = columnFill
      setFit((f) =>
        f.level === level && f.pages === pages ? f : { level, pages }
      )
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(box)
    return () => ro.disconnect()
  }, [layoutKey])
  return { outer, inner, ...fit }
}

/** Keeps the screen on while the page is visible (where the browser can). */
function useWakeLock() {
  useEffect(() => {
    let lock: { release: () => Promise<void> } | null = null
    let stopped = false
    async function take() {
      try {
        const wl = (
          navigator as Navigator & {
            wakeLock?: {
              request: (
                t: "screen"
              ) => Promise<{ release: () => Promise<void> }>
            }
          }
        ).wakeLock
        if (!wl || document.visibilityState !== "visible") return
        const l = await wl.request("screen")
        if (stopped) void l.release()
        else lock = l
      } catch {}
    }
    void take()
    // The lock is dropped when the tab is hidden: take it again on return.
    const onVis = () => void take()
    document.addEventListener("visibilitychange", onVis)
    return () => {
      stopped = true
      document.removeEventListener("visibilitychange", onVis)
      void lock?.release().catch(() => {})
    }
  }, [])
}

/** True when the pointer has not moved for a few seconds. */
function useIdlePointer(ms = 3000) {
  const [idle, setIdle] = useState(false)
  useEffect(() => {
    let timer = setTimeout(() => setIdle(true), ms)
    const move = () => {
      setIdle(false)
      clearTimeout(timer)
      timer = setTimeout(() => setIdle(true), ms)
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("keydown", move)
    return () => {
      clearTimeout(timer)
      window.removeEventListener("pointermove", move)
      window.removeEventListener("keydown", move)
    }
  }, [ms])
  return idle
}
