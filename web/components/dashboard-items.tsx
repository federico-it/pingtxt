"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { UserCheckIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { ExpiryBadges } from "@/components/expiry"
import { useAckName } from "@/components/incident-ack"
import { TagList } from "@/components/labels"
import { PinButton } from "@/components/pin-button"
import { useReasonLine } from "@/components/reasons"
import { StatusGlyph } from "@/components/status"
import { Checkbox } from "@/components/ui/checkbox"
import { UptimeBars } from "@/components/uptime-bars"
import type { MonitorSummary, Status, Tag } from "@/lib/api"
import { ago, datetime, displayTarget, interval, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

/** Marks next to a monitor's name: the monitor blocking it, who acknowledged its incident. */
export function MonitorMarks({ m, short = false }: { m: MonitorSummary; short?: boolean }) {
  const t = useTranslations("dashboard")
  const ackName = useAckName()
  return (
    <>
      {m.blocked_by && (
        <span className="shrink-0 truncate text-xs text-muted-foreground" title={t("blockedBy", { name: m.blocked_by.name })}>
          ⛓{short ? "" : ` ${m.blocked_by.name}`}
        </span>
      )}
      {!!m.incident?.acked_at && (
        <span
          className="inline-flex shrink-0 items-center gap-1 text-xs text-info"
          title={t("ackedBy", { name: ackName(m.incident.acked_by) })}
        >
          <UserCheckIcon className="size-3" />
          {!short && <span className="hidden sm:inline">{ackName(m.incident.acked_by)}</span>}
        </span>
      )}
    </>
  )
}

/** The line under a monitor's name: why it fails, or what it checks. */
export function MonitorSubline({ m, now, className }: { m: MonitorSummary; now: number; className?: string }) {
  const t = useTranslations("dashboard")
  const reasonLine = useReasonLine()
  if (!m.paused && (m.status === "degraded" || m.status === "down") && (m.reasons?.length ?? 0) > 0)
    return (
      <div
        className={cn("truncate text-xs", m.status === "down" ? "text-down" : "text-warn", className)}
        title={m.reasons!.map((r) => `${r.message} — ${r.locations.join(", ")}`).join("\n")}
      >
        {m.short_reason || reasonLine(m.reasons!)}
        <ExpiryBadges checks={m.expiry} now={now} />
      </div>
    )
  return (
    <div className={cn("truncate text-xs text-muted-foreground", className)}>
      {m.type === "push"
        ? t("heartbeatLine", { every: interval(m.interval_sec) })
        : m.type === "server"
          ? // The last check's numbers say more than the
            // mount points: "cpu 21%, ram 48%, disk / 42%..."
            m.last?.ok && m.last.message
            ? t("serverLine", { metrics: m.last.message, every: interval(m.interval_sec) })
            : t("serverLineWaiting", { every: interval(m.interval_sec) })
          : t("monitorLine", { type: m.type, target: displayTarget(m.target), every: interval(m.interval_sec) })}
      {m.locations.length > 1 && ` · ${t("locations", { count: m.locations.length })}`}
      <ExpiryBadges checks={m.expiry} now={now} />
    </div>
  )
}

/** Color for a 24h uptime: yellow under 99%, red under 90%. */
export function uptimeClass(u: number) {
  return cn(u >= 0 && u < 0.99 && "text-warn", u >= 0 && u < 0.9 && "text-down")
}

const dotClass: Record<Status, string> = {
  down: "bg-down",
  degraded: "bg-warn",
  maintenance: "bg-info",
  pending: "bg-muted-foreground/40",
  up: "bg-up",
  paused: "bg-muted-foreground/25",
}
const dotOrder: Status[] = ["down", "degraded", "maintenance", "pending", "up", "paused"]

/** One small square per monitor, worst first: a group's health at a glance,
 * also when it is folded. Long groups end with "+N". */
export function StatusDots({ list, max = 24, className }: { list: MonitorSummary[]; max?: number; className?: string }) {
  const ts = useTranslations("status")
  const sorted = [...list].sort((a, b) => dotOrder.indexOf(a.status) - dotOrder.indexOf(b.status))
  const counts = new Map<Status, number>()
  for (const m of list) counts.set(m.status, (counts.get(m.status) ?? 0) + 1)
  const title = dotOrder
    .filter((s) => counts.has(s))
    .map((s) => `${ts(s)}: ${counts.get(s)}`)
    .join(" · ")
  return (
    <span aria-hidden title={title} className={cn("inline-flex shrink-0 items-center gap-0.5", className)}>
      {sorted.slice(0, max).map((m) => (
        <span key={m.id} className={cn("size-1.5", dotClass[m.status] ?? dotClass.pending)} />
      ))}
      {sorted.length > max && <span className="ml-0.5 text-[10px] text-muted-foreground">+{sorted.length - max}</span>}
    </span>
  )
}

/** A monitor as a card for the dashboard's card view. */
export function MonitorCard({
  m,
  group,
  groupTitle,
  now,
  tagById,
  pinned,
  selected,
  selecting,
  onSelect,
}: {
  m: MonitorSummary
  /** subgroup it is in, below the block's group */
  group?: string
  /** the subgroup's full path, on hover */
  groupTitle?: string
  now: number
  tagById: Map<number, Tag>
  pinned: boolean
  selected: boolean
  /** some monitor is selected: checkboxes stay visible */
  selecting: boolean
  /** admins: select the card for bulk actions */
  onSelect?: (on: boolean) => void
}) {
  const t = useTranslations("dashboard")
  const ts = useTranslations("status")
  const router = useRouter()
  return (
    <div
      className={cn(
        "group/card relative flex min-w-0 flex-col gap-2.5 border border-dashed p-3 transition-colors hover:bg-accent/50",
        m.status === "down" && "border-down/70 bg-down/5",
        m.status === "degraded" && "border-warn/70",
        selected && "bg-accent/50"
      )}
    >
      <div className="flex min-w-0 items-start gap-2">
        <div className="min-w-0 flex-1">
          {group && (
            <div className="mb-0.5 truncate text-[11px] text-muted-foreground" title={groupTitle}>
              {"└ "}
              {group}
            </div>
          )}
          <div className="flex min-w-0 items-center gap-2">
            <StatusGlyph status={m.status} className="shrink-0" />
            {/* The name's link covers the whole card; controls sit above it. */}
            <Link
              href={`/monitors/view/?id=${m.id}`}
              className="truncate font-medium outline-none after:absolute after:inset-0 focus-visible:after:ring-2 focus-visible:after:ring-ring/50"
            >
              {m.name}
            </Link>
            <MonitorMarks m={m} short />
          </div>
          <MonitorSubline m={m} now={now} className="mt-0.5" />
        </div>
        {onSelect && (
          <Checkbox
            checked={selected}
            onCheckedChange={(v) => onSelect(v)}
            aria-label={t("bulk.select", { name: m.name })}
            className={cn(
              "relative z-10 mt-0.5 shrink-0",
              !selecting && !selected && "opacity-0 group-hover/card:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
            )}
          />
        )}
        <PinButton id={m.id} name={m.name} pinned={pinned} className="relative z-10 -mt-1.5 -mr-1.5 size-8 self-start" />
      </div>
      <UptimeBars
        bars={m.bars}
        height="h-4"
        className="relative z-10"
        onSelect={(from, to) => router.push(`/monitors/view/?id=${m.id}&from=${from}&to=${to}#log`)}
      />
      <div className="mt-auto flex min-w-0 items-center gap-3 text-xs text-muted-foreground">
        <span className={cn("text-foreground", uptimeClass(m.uptime_24h))} title={t("cols.uptime")}>
          {pct(m.uptime_24h)}
        </span>
        {/* a server check's run time is no response time */}
        {m.type !== "server" && <span title={t("cols.avg")}>{ms(m.avg_ms_24h)}</span>}
        <TagList ids={m.tag_ids} tagById={tagById} className="min-w-0 flex-nowrap overflow-hidden" />
        <span className="ml-auto shrink-0" title={m.last?.ts ? datetime(m.last.ts) : undefined}>
          {m.paused ? ts("paused") : ago(m.last?.ts)}
        </span>
      </div>
    </div>
  )
}
