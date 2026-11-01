"use client"

import { useEffect, useState } from "react"

import { useLang, useT } from "@/components/lang"
import { SectionTitle } from "@/components/site-shell"
import { localePath } from "@/lib/i18n"

// Totals from the opt-in usage stats of self-hosted hubs, served by
// pingtxt-telemetry (public JSON, cross-origin reads allowed).
const STATS_URL = "https://telemetry.pingtxt.com/"
const STATS_API = "https://telemetry.pingtxt.com/v1/stats"

type Live = { instances: number; monitors: number; agents: number }

function useLive(): Live | null {
  const [live, setLive] = useState<Live | null>(null)
  useEffect(() => {
    let alive = true
    fetch(STATS_API)
      .then((res) => (res.ok ? res.json() : null))
      .then((s) => {
        if (alive && s?.live) setLive(s.live)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])
  return live
}


// UsageStats shows live hubs, monitors and agents. Hidden until the service
// answers with at least one hub.
export function UsageStats() {
  const live = useLive()
  const lang = useLang()
  const t = useT()
  if (!live || live.instances < 1) return null
  const items: [number, string][] = [
    [live.instances, t("hubs running", "hub attivi")],
    [live.monitors, t("monitors", "monitor")],
    [live.agents, t("agents", "agent")],
  ]
  return (
    <section className="space-y-6">
      <SectionTitle>{t("in the wild", "in giro per il mondo")}</SectionTitle>
      <div className="grid gap-3 sm:grid-cols-3">
        {items.map(([n, label]) => (
          <div key={label} className="border border-dashed p-4">
            <div className="text-2xl font-semibold">{n.toLocaleString(t("en-US", "it-IT"))}</div>
            <div className="text-xs text-muted-foreground">{label}</div>
          </div>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        {t(
          "Live numbers from hubs whose admins chose to share anonymous usage stats.",
          "Numeri in tempo reale dagli hub i cui admin hanno scelto di condividere statistiche d'uso anonime.",
        )}{" "}
        <a href={localePath(lang, "/docs/#usage-stats")} className="underline underline-offset-4 hover:text-foreground">
          {t("what is sent", "cosa viene inviato")}
        </a>{" "}
        ·{" "}
        <a href={STATS_URL} className="underline underline-offset-4 hover:text-foreground">
          {t("all stats", "tutte le statistiche")}
        </a>
      </p>
    </section>
  )
}
