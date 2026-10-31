"use client"

import { useTranslations } from "use-intl"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { Bar } from "@/lib/api"
import { datetime, utcDate, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

// Share of failed checks in a bucket that turns it yellow, and red past the
// outage line (same >50% rule the hub uses to call a monitor down). A single
// missed check out of ~150 stays green.
const WARN_FAILED = 0.2
const DOWN_FAILED = 0.5

function barClass(b: Bar) {
  if (b.total === 0) return b.maint ? "bg-info" : "bg-muted"
  const failed = (b.total - b.ok) / b.total
  if (failed > DOWN_FAILED) return "bg-down"
  if (failed >= WARN_FAILED) return "bg-warn"
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
            {b.total > 0 && b.maint > 0 && <div className="text-info">{t("maintChecks", { count: b.maint })}</div>}
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}
