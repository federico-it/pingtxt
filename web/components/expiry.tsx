"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { api, type ExpiryCheck } from "@/lib/api"
import { ago } from "@/lib/format"
import { cn } from "@/lib/utils"

const DAY = 86_400_000

/** Whole days until expiry, negative once expired, null when unknown. */
export function expiryDays(e: ExpiryCheck, now: number): number | null {
  if (!e.expires_at || !now) return null
  return Math.floor((e.expires_at - now) / DAY)
}

function tone(days: number | null) {
  if (days === null) return "text-muted-foreground"
  if (days < 0 || days <= 7) return "text-down"
  if (days <= 30) return "text-warn"
  return ""
}

export function daysLabel(days: number | null): string {
  if (days === null) return "unknown"
  if (days < 0) return `expired ${-days}d ago`
  if (days === 0) return "expires today"
  return `${days}d left`
}

function date(ms: number) {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

const labels = { cert: "tls cert", domain: "domain" } as const

/** Compact "cert 5d" markers for monitors expiring within 30 days. */
export function ExpiryBadges({
  checks,
  now,
}: {
  checks: ExpiryCheck[] | undefined
  now: number
}) {
  const soon = (checks ?? [])
    .map((e) => ({ e, days: expiryDays(e, now) }))
    .filter(
      (x): x is { e: ExpiryCheck; days: number } =>
        x.days !== null && x.days <= 30
    )
  if (!soon.length) return null
  return (
    <>
      {soon.map(({ e, days }) => (
        <span key={e.kind} className={cn("ml-2", tone(days))}>
          {e.kind} {days < 0 ? "expired" : `${days}d`}
        </span>
      ))}
    </>
  )
}

/** Certificate and domain expiry of one monitor, with a "check now" action. */
export function ExpiryTable({
  monitorId,
  checks,
  now,
  onChange,
}: {
  monitorId: number
  checks: ExpiryCheck[]
  now: number
  onChange: () => void
}) {
  const [busy, setBusy] = useState(false)

  async function refresh() {
    setBusy(true)
    try {
      await api(`/api/monitors/${monitorId}/expiry`, { method: "POST" })
      onChange()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "error")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-2">
      {checks.map((e) => {
        const days = expiryDays(e, now)
        return (
          <div
            key={e.kind}
            className="grid gap-x-4 gap-y-0.5 text-xs sm:grid-cols-[80px_1fr_auto]"
          >
            <span className="text-muted-foreground">{labels[e.kind]}</span>
            <div className="min-w-0">
              <span className="text-foreground">{e.subject}</span>
              {e.expires_at > 0 && (
                <>
                  {" · "}
                  <span className={tone(days)}>{daysLabel(days)}</span>
                  <span className="text-muted-foreground">
                    {" "}
                    ({date(e.expires_at)})
                  </span>
                </>
              )}
              {e.issuer && (
                <span className="text-muted-foreground">
                  {" "}
                  · {e.kind === "cert" ? "issuer" : "registrar"} {e.issuer}
                </span>
              )}
              {e.error && (
                <div
                  className={cn(
                    "truncate",
                    e.expires_at ? "text-warn" : "text-down"
                  )}
                >
                  ! {e.error}
                </div>
              )}
            </div>
            <span className="text-muted-foreground sm:text-right">
              {e.checked_at
                ? `${e.source ? e.source + " · " : ""}checked ${ago(e.checked_at)}`
                : "waiting for the first lookup"}
            </span>
          </div>
        )
      })}
      <div>
        <Button variant="outline" size="sm" onClick={refresh} disabled={busy}>
          {busy ? "checking..." : "check now"}
        </Button>
      </div>
    </div>
  )
}
