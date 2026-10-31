"use client"

import { useEffect, useState } from "react"

// The status page of the official push relay (push.pingtxt.com), served by a
// pingtxt hub. Its public JSON allows cross-origin reads.
export const STATUS_URL = "https://status.pingtxt.com/"
const STATUS_API = "https://status.pingtxt.com/api/public/status"
const REFRESH_MS = 60_000

type State = "operational" | "degraded" | "partial_outage" | "major_outage" | "unknown"

const states: Record<State, { color: string; label: string }> = {
  operational: { color: "text-up", label: "all systems operational" },
  degraded: { color: "text-warn", label: "degraded performance" },
  partial_outage: { color: "text-down", label: "partial outage" },
  major_outage: { color: "text-down", label: "major outage" },
  unknown: { color: "text-muted-foreground", label: "status" },
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
  const s = states[useStatus()]
  return (
    <a href={STATUS_URL} className={className} title={s.label}>
      <span className={s.color} aria-hidden>
        ●
      </span>{" "}
      status<span className="sr-only">: {s.label}</span>
    </a>
  )
}

// StatusLine is the footer version: the dot and the state in words.
export function StatusLine() {
  const s = states[useStatus()]
  return (
    <a href={STATUS_URL} className="inline-flex items-center gap-1.5 underline-offset-4 hover:text-foreground hover:underline">
      <span className={s.color} aria-hidden>
        ●
      </span>
      {s.label === "status" ? "service status" : s.label}
    </a>
  )
}
