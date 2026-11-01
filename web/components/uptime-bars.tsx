"use client"

import { useTranslations } from "use-intl"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { Bar } from "@/lib/api"
import { datetime, utcDate, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

// ok counts the checks taken while the monitor was up or degraded, so a
// single failing location does not show here. Share of checks taken while the
// monitor was down (or degraded) that turns a bar yellow, and red past half.
const WARN_SHARE = 0.2
const DOWN_SHARE = 0.5

function barClass(b: Bar) {
  if (b.total === 0) return b.maint ? "bg-info" : "bg-muted"
  const down = (b.total - b.ok) / b.total
  if (down > DOWN_SHARE) return "bg-down"
  if (down + (b.degraded ?? 0) / b.total >= WARN_SHARE) return "bg-warn"
  return "bg-up"
}

export function UptimeBars({
  bars,
  className,
  height = "h-5",
  daily = false,
  onSelect,
  selected,
}: {
  bars: Bar[]
  className?: string
  height?: string
  daily?: boolean
  /** Makes the slots clickable: gets the slot's start and end (unix ms). */
  onSelect?: (from: number, to: number) => void
  /** Start of the highlighted slot. */
  selected?: number
}) {
  const t = useTranslations("uptimeBars")
  // Slot length, from the spacing of the bars (one hour by default).
  const step = bars.length > 1 ? bars[1].t - bars[0].t : 3600_000
  return (
    <div className={cn("flex items-stretch gap-px", height, className)}>
      {bars.map((b) => (
        <Tooltip key={b.t}>
          <TooltipTrigger
            render={
              onSelect ? (
                <button
                  type="button"
                  aria-label={datetime(b.t)}
                  onClick={(e) => {
                    // The bars can sit inside a link (dashboard rows).
                    e.preventDefault()
                    e.stopPropagation()
                    onSelect(b.t, b.t + step)
                  }}
                  className={cn(
                    "min-w-0.5 flex-1 cursor-pointer opacity-90 transition-opacity hover:opacity-100",
                    barClass(b),
                    selected !== undefined && selected !== b.t && "opacity-40",
                    selected === b.t && "opacity-100 ring-1 ring-foreground"
                  )}
                />
              ) : (
                <span className={cn("min-w-0.5 flex-1 opacity-90 transition-opacity hover:opacity-100", barClass(b))} />
              )
            }
          />
          <TooltipContent className="font-mono text-xs">
            <div>{daily ? utcDate(b.t) : datetime(b.t)}</div>
            <div>
              {b.total === 0
                ? b.maint
                  ? t("maintenance")
                  : t("noData")
                : t("summary", { pct: pct(b.ok / b.total), ok: b.ok, total: b.total, avg: ms(b.avg_ms) })}
            </div>
            {b.total > 0 && !!b.degraded && <div className="text-warn">{t("degradedChecks", { count: b.degraded })}</div>}
            {b.total > 0 && b.maint > 0 && <div className="text-info">{t("maintChecks", { count: b.maint })}</div>}
            {onSelect && <div className="text-muted-foreground">{t("clickForChecks")}</div>}
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}
