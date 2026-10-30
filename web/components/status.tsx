import { cn } from "@/lib/utils"
import type { Status } from "@/lib/api"

const map: Record<Status, { glyph: string; label: string; cls: string }> = {
  up: { glyph: "●", label: "up", cls: "text-up" },
  down: { glyph: "●", label: "down", cls: "text-down" },
  degraded: { glyph: "◐", label: "degraded", cls: "text-warn" },
  paused: { glyph: "○", label: "paused", cls: "text-muted-foreground" },
  pending: { glyph: "◌", label: "pending", cls: "text-muted-foreground" },
}

export function StatusText({ status, className }: { status: Status; className?: string }) {
  const s = map[status] ?? map.pending
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap", s.cls, className)}>
      <span className={cn(status === "down" && "animate-pulse")}>{s.glyph}</span>
      <span>{s.label}</span>
    </span>
  )
}

export function StatusGlyph({ status, className }: { status: Status; className?: string }) {
  const s = map[status] ?? map.pending
  return <span className={cn(s.cls, status === "down" && "animate-pulse", className)}>{s.glyph}</span>
}

export function OkText({ ok }: { ok: boolean }) {
  return ok ? <span className="text-up">ok</span> : <span className="text-down">fail</span>
}
