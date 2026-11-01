"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { api, useApi, type TelemetryStatus } from "@/lib/api"

export const TELEMETRY_DOCS = "https://pingtxt.com/docs/#usage-stats"

/** Usage stats status for admins (null for read-only users). */
export function useTelemetry(isAdmin: boolean) {
  return useApi<TelemetryStatus>(isAdmin ? "/api/telemetry" : null)
}

export async function setTelemetry(enabled: boolean) {
  return api<TelemetryStatus>("/api/telemetry", {
    method: "PUT",
    body: { enabled },
  })
}

/**
 * Asks admins once whether the hub may send anonymous usage stats. Nothing is
 * sent until someone says yes; the answer can be changed in the settings.
 */
export function TelemetryNotice({ isAdmin }: { isAdmin: boolean }) {
  const t = useTranslations("telemetry")
  const s = useTelemetry(isAdmin)
  const [busy, setBusy] = useState(false)
  if (!s.data || s.data.asked || s.data.forced_off) return null

  async function answer(enabled: boolean) {
    setBusy(true)
    try {
      await s.mutate(await setTelemetry(enabled), { revalidate: false })
      toast.success(enabled ? t("thanks") : t("declined"))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="-mx-4 flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-dashed bg-info/5 px-4 py-2 text-xs sm:-mx-6 sm:px-6">
      <span className="min-w-0 flex-1 basis-64">
        <span className="text-info">?</span> {t("question")}{" "}
        <a
          href={TELEMETRY_DOCS}
          target="_blank"
          rel="noreferrer"
          className="text-muted-foreground underline underline-offset-4 hover:text-foreground"
        >
          {t("what")}
        </a>
      </span>
      <span className="flex gap-2">
        <Button size="xs" disabled={busy} onClick={() => answer(true)}>
          {t("yes")}
        </Button>
        <Button
          size="xs"
          variant="ghost"
          disabled={busy}
          onClick={() => answer(false)}
        >
          {t("no")}
        </Button>
      </span>
    </div>
  )
}
