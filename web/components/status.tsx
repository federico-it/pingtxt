"use client"

import { useTranslations } from "use-intl"

import { cn } from "@/lib/utils"
import type { Status } from "@/lib/api"

const map: Record<Status, { glyph: string; cls: string }> = {
  up: { glyph: "●", cls: "text-up" },
  down: { glyph: "●", cls: "text-down" },
  degraded: { glyph: "◐", cls: "text-warn" },
  paused: { glyph: "○", cls: "text-muted-foreground" },
  pending: { glyph: "◌", cls: "text-muted-foreground" },
  maintenance: { glyph: "◇", cls: "text-info" },
}

export function StatusText({ status, className }: { status: Status; className?: string }) {
  const t = useTranslations("status")
  const s = map[status] ?? map.pending
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap", s.cls, className)}>
      <span className={cn(status === "down" && "animate-pulse")}>{s.glyph}</span>
      <span>{t(map[status] ? status : "pending")}</span>
    </span>
  )
}

export function StatusGlyph({ status, className }: { status: Status; className?: string }) {
  const s = map[status] ?? map.pending
  return <span className={cn(s.cls, status === "down" && "animate-pulse", className)}>{s.glyph}</span>
}

export function OkText({ ok }: { ok: boolean }) {
  const t = useTranslations("status")
  return ok ? <span className="text-up">{t("ok")}</span> : <span className="text-down">{t("fail")}</span>
}
