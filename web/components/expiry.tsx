"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { api, useIsAdmin, type ExpiryCheck } from "@/lib/api"
import { ago } from "@/lib/format"
import { tr } from "@/lib/i18n"
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
  if (days === null) return tr("expiry.unknown")
  if (days < 0) return tr("expiry.expiredAgo", { days: -days })
  if (days === 0) return tr("expiry.today")
  return tr("expiry.left", { days })
}

function date(ms: number) {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}


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
  return <ExpiryBadgeList soon={soon} />
}

function ExpiryBadgeList({ soon }: { soon: { e: ExpiryCheck; days: number }[] }) {
  const t = useTranslations("expiry")
  return (
    <>
      {soon.map(({ e, days }) => (
        <span key={e.kind} className={cn("ml-2", tone(days))}>
          {t(`badge.${e.kind}`)} {days < 0 ? t("expired") : t("days", { days })}
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
  const isAdmin = useIsAdmin()
  const t = useTranslations("expiry")

  async function refresh() {
    setBusy(true)
    try {
      await api(`/api/monitors/${monitorId}/expiry`, { method: "POST" })
      onChange()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("error"))
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
            <span className="text-muted-foreground">{t(`kind.${e.kind}`)}</span>
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
                  · {e.kind === "cert" ? t("issuer") : t("registrar")} {e.issuer}
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
                ? `${e.source ? e.source + " · " : ""}${t("checked", { ago: ago(e.checked_at) })}`
                : t("waiting")}
            </span>
          </div>
        )
      })}
      {isAdmin && (
        <div>
          <Button variant="outline" onClick={refresh} disabled={busy}>
            {busy ? t("checking") : t("checkNow")}
          </Button>
        </div>
      )}
    </div>
  )
}
