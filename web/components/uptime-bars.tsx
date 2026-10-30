import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { Bar } from "@/lib/api"
import { datetime, ms, pct } from "@/lib/format"
import { cn } from "@/lib/utils"

function barClass(b: Bar) {
  if (b.total === 0) return "bg-muted"
  const r = b.ok / b.total
  if (r >= 0.999) return "bg-up"
  if (r >= 0.9) return "bg-warn"
  return "bg-down"
}

export function UptimeBars({ bars, className, height = "h-5" }: { bars: Bar[]; className?: string; height?: string }) {
  return (
    <div className={cn("flex items-stretch gap-px", height, className)}>
      {bars.map((b) => (
        <Tooltip key={b.t}>
          <TooltipTrigger
            render={<span className={cn("min-w-0.5 flex-1 opacity-90 transition-opacity hover:opacity-100", barClass(b))} />}
          />
          <TooltipContent className="font-mono text-xs">
            <div>{datetime(b.t)}</div>
            <div>
              {b.total === 0 ? "no data" : `${pct(b.ok / b.total)} · ${b.ok}/${b.total} ok · avg ${ms(b.avg_ms)}`}
            </div>
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}
