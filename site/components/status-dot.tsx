"use client"

import { useEffect, useState } from "react"

import { useT } from "@/components/lang"

// The status page of the official push relay (push.pingtxt.com), served by a
// pingtxt hub. Its public JSON allows cross-origin reads.
export const STATUS_URL = "https://status.pingtxt.com/"
const STATUS_API = "https://status.pingtxt.com/api/public/status"
const REFRESH_MS = 60_000

type State = "operational" | "degraded" | "partial_outage" | "major_outage" | "unknown"

const states: Record<State, { color: string; en: string; it: string }> = {
  operational: { color: "text-up", en: "all systems operational", it: "tutti i sistemi operativi" },
  degraded: { color: "text-warn", en: "degraded performance", it: "prestazioni ridotte" },
  partial_outage: { color: "text-down", en: "partial outage", it: "disservizio parziale" },
  major_outage: { color: "text-down", en: "major outage", it: "disservizio grave" },
  unknown: { color: "text-muted-foreground", en: "status", it: "stato" },
}

function useLabel(s: State) {
  const t = useT()
  return t(states[s].en, states[s].it)
}

function useStatus(): State {
  const [state, setState] = useState<State>("unknown")
  useEffect(() => {
    let alive = true
    async function load() {
      if (document.visibilityState === "hidden") return
      try {
        const res = await fetch(STATUS_API, { cache: "no-store" })
        const s = res.ok ? (await res.json()).status : "unknown"
        if (alive) setState(s in states ? s : "unknown")
      } catch {
        if (alive) setState("unknown")
      }
    }
    load()
    const timer = setInterval(load, REFRESH_MS)
    document.addEventListener("visibilitychange", load)
    return () => {
      alive = false
      clearInterval(timer)
      document.removeEventListener("visibilitychange", load)
    }
  }, [])
  return state
}

// StatusLink is the "status" entry of the header, with a colored dot.
export function StatusLink({ className }: { className?: string }) {
  const state = useStatus()
  const s = states[state]
  const label = useLabel(state)
  return (
    <a href={STATUS_URL} className={className} title={label}>
      <span className={s.color} aria-hidden>
        ●
      </span>{" "}
      status<span className="sr-only">: {label}</span>
    </a>
  )
}

// StatusLine is the footer version: the dot and the state in words.
export function StatusLine() {
  const state = useStatus()
  const s = states[state]
  const label = useLabel(state)
  const t = useT()
  return (
    <a href={STATUS_URL} className="inline-flex items-center gap-1.5 underline-offset-4 hover:text-foreground hover:underline">
      <span className={s.color} aria-hidden>
        ●
      </span>
      {state === "unknown" ? t("service status", "stato dei servizi") : label}
    </a>
  )
}
