"use client"

import { useState } from "react"
import { UserCheckIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { segment } from "@/components/ui/control"
import { api, useApi, useIsAdmin, type Me } from "@/lib/api"
import { ago, datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

/** The acknowledgement of an open incident, as the API returns it. */
export interface AckState {
  id: number
  acked_at: number
  acked_by: string
}

/** Who acknowledged an incident: "you" or the email. */
export function useAckName() {
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 60_000 })
  const t = useTranslations("ack")
  return (email: string) => (me.data && me.data.user.email === email ? t("you") : email)
}

/** "a@b.c is on it" with a check mark, or nothing when nobody acknowledged it. */
export function AckNote({ incident, className }: { incident: AckState; className?: string }) {
  const t = useTranslations("ack")
  const name = useAckName()
  if (!incident.acked_at) return null
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1 text-info", className)} title={datetime(incident.acked_at)}>
      <UserCheckIcon className="size-3 shrink-0" />
      <span className="truncate">{t("onIt", { name: name(incident.acked_by), ago: ago(incident.acked_at) })}</span>
    </span>
  )
}

/**
 * Button to acknowledge an open incident (admins only): "acknowledge" when
 * nobody is on it, "release" for the user who acknowledged it, "take over"
 * for the others.
 */
export function AckButton({ incident, onChange }: { incident: AckState; onChange?: () => void }) {
  const t = useTranslations("ack")
  const isAdmin = useIsAdmin()
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 60_000 })
  const [busy, setBusy] = useState(false)
  if (!isAdmin) return null
  const mine = !!incident.acked_at && me.data?.user.email === incident.acked_by
  const label = !incident.acked_at ? t("ack") : mine ? t("release") : t("takeOver")
  const hint = !incident.acked_at || !mine ? t("ackHint") : t("releaseHint")

  async function run() {
    setBusy(true)
    try {
      await api(`/api/incidents/${incident.id}/ack`, { method: mine ? "DELETE" : "POST" })
      toast.success(mine ? t("released") : t("acked"))
      onChange?.()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <button onClick={run} disabled={busy} title={hint} className={segment({ active: !incident.acked_at, size: "xs" })}>
      {!mine && <UserCheckIcon className="mr-1 size-3" />}
      {label}
    </button>
  )
}
