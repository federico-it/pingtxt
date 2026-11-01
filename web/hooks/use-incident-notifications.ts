import { useEffect, useRef } from "react"
import { useRouter } from "next/navigation"
import { useTranslations } from "use-intl"

import { notify, useBrowserNotify } from "@/lib/browser-notify"
import { useApi, type Incident } from "@/lib/api"
import { duration } from "@/lib/format"

/**
 * Watches the open incidents while a hub tab is open and shows a desktop
 * notification when a monitor goes down or comes back up. The first answer
 * is the baseline: incidents already open are not announced.
 */
export function useIncidentNotifications(signedIn: boolean) {
  const on = useBrowserNotify() === "on"
  const router = useRouter()
  const t = useTranslations("browserNotify")
  const open = useApi<Incident[]>(signedIn && on ? "/api/incidents?state=open" : null, {
    refreshInterval: 15_000,
  })
  const known = useRef<Map<number, Incident> | null>(null)

  useEffect(() => {
    if (!on) {
      known.current = null
      return
    }
    if (!open.data) return
    const now = new Map(open.data.map((i) => [i.id, i]))
    const before = known.current
    known.current = now
    if (!before) return
    const view = (id: number) => () => router.push(`/monitors/view/?id=${id}`)
    for (const [id, i] of now) {
      if (before.has(id)) continue
      void notify(t("down", { name: i.monitor_name }), {
        body: i.cause || t("downBody"),
        tag: `incident-${id}`,
        onClick: view(i.monitor_id),
      })
    }
    for (const [id, i] of before) {
      if (now.has(id)) continue
      void notify(t("up", { name: i.monitor_name }), {
        body: t("upBody", { duration: duration(Date.now() - i.started_at) }),
        tag: `incident-${id}`,
        onClick: view(i.monitor_id),
      })
    }
  }, [open.data, on, router, t])
}
