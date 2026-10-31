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
}: {
  bars: Bar[]
  className?: string
  height?: string
  daily?: boolean
}) {
  const t = useTranslations("uptimeBars")
  return (
    <div className={cn("flex items-stretch gap-px", height, className)}>
      {bars.map((b) => (
        <Tooltip key={b.t}>
          <TooltipTrigger
            render={<span className={cn("min-w-0.5 flex-1 opacity-90 transition-opacity hover:opacity-100", barClass(b))} />}
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
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}
