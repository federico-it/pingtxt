"use client"

import { useEffect, useState } from "react"

import { SectionTitle } from "@/components/site-shell"

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

const fmt = (n: number) => n.toLocaleString("en-US")

// UsageStats shows live hubs, monitors and agents. Hidden until the service
// answers with at least one hub.
export function UsageStats() {
  const live = useLive()
  if (!live || live.instances < 1) return null
  const items: [number, string][] = [
    [live.instances, "hubs running"],
    [live.monitors, "monitors"],
    [live.agents, "agents"],
  ]
  return (
    <section className="space-y-6">
      <SectionTitle>in the wild</SectionTitle>
      <div className="grid gap-3 sm:grid-cols-3">
        {items.map(([n, label]) => (
          <div key={label} className="border border-dashed p-4">
            <div className="text-2xl font-semibold">{fmt(n)}</div>
            <div className="text-xs text-muted-foreground">{label}</div>
          </div>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        Live numbers from hubs whose admins chose to share anonymous usage stats.{" "}
        <a href="/docs/#usage-stats" className="underline underline-offset-4 hover:text-foreground">
          what is sent
        </a>{" "}
        ·{" "}
        <a href={STATS_URL} className="underline underline-offset-4 hover:text-foreground">
          all stats
        </a>
      </p>
    </section>
  )
}
