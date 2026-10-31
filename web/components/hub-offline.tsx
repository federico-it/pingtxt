"use client"

import { useEffect, useRef, useState } from "react"
import { useSWRConfig } from "swr"
import { useTranslations } from "use-intl"

import { HUB_OFFLINE_EVENT } from "@/lib/api"
import { cn } from "@/lib/utils"

/** /api/health: {ok, version} when ready, {starting, phase, ...} while booting. */
interface Health {
  ok: boolean
  version?: string
  starting?: boolean
  phase?: "restore" | "database" | "starting"
  previous_version?: string
}

type Probe =
  | { kind: "up"; version: string }
  | { kind: "starting"; health: Health }
  | { kind: "down" }

async function probe(): Promise<Probe> {
  try {
    const ctl = new AbortController()
    const timer = setTimeout(() => ctl.abort(), 4000)
    const res = await fetch("/api/health", {
      cache: "no-store",
      signal: ctl.signal,
    })
    clearTimeout(timer)
    const h = (await res.json().catch(() => null)) as Health | null
    if (res.ok && h?.ok) return { kind: "up", version: h.version ?? "" }
    if (h?.starting) return { kind: "starting", health: h }
  } catch {}
  return { kind: "down" }
}

const FRAMES = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
const PULSE =
  "M0 30H120l10-6 8 6h18l8-26 10 40 8-14h28l10-8 10 8H330l10-6 8 6h18l8-26 10 40 8-14h28l10-8 10 8H500"

/**
 * Covers the app while the hub restarts or updates, and goes on by itself
 * when it is back: the data is refreshed, or the page reloaded when the hub
 * now runs another version (new UI files).
 */
export function HubOffline() {
  const t = useTranslations("hubOffline")
  const { mutate } = useSWRConfig()
  const [state, setState] = useState<Probe | null>(null) // null: hub up
  const [since, setSince] = useState(0)
  const [now, setNow] = useState(0)
  const version = useRef<string | null>(null)
  const checking = useRef(false)
  const isDown = useRef(false)

  // Remember the version the page was loaded with.
  useEffect(() => {
    probe().then((p) => {
      if (p.kind === "up") version.current = p.version
    })
  }, [])

  // A failed API call: check the hub before covering the page.
  useEffect(() => {
    async function onOffline() {
      if (checking.current) return
      checking.current = true
      const p = await probe()
      checking.current = false
      if (p.kind === "up") return
      if (!isDown.current) {
        isDown.current = true
        setSince(Date.now())
        setNow(Date.now())
      }
      setState(p)
    }
    window.addEventListener(HUB_OFFLINE_EVENT, onOffline)
    return () => window.removeEventListener(HUB_OFFLINE_EVENT, onOffline)
  }, [])

  const down = state !== null
  useEffect(() => {
    if (!down) return
    let stop = false
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      const p = await probe()
      if (stop) return
      if (p.kind === "up") {
        if (version.current && p.version !== version.current) {
          window.location.reload()
          return
        }
        version.current = p.version
        isDown.current = false
        setState(null)
        mutate(() => true) // refresh every loaded list
        return
      }
      setState(p)
      timer = setTimeout(poll, 1500)
    }
    timer = setTimeout(poll, 1500)
    const tick = setInterval(() => setNow(Date.now()), 120)
    return () => {
      stop = true
      clearTimeout(timer)
      clearInterval(tick)
    }
  }, [down, mutate])

  if (!state || state.kind === "up") return null

  const h = state.kind === "starting" ? state.health : null
  const updating = !!h?.previous_version && h.previous_version !== h.version
  const frame = FRAMES[Math.floor(now / 120) % FRAMES.length]
  let steps: { label: string; st: "done" | "now" | "" }[]
  if (h) {
    const order =
      h.phase === "restore"
        ? ["restore", "database", "starting"]
        : ["database", "starting"]
    const at = order.indexOf(h.phase ?? "database")
    steps = order.map((p, i) => ({
      label:
        p === "database" && updating
          ? t("steps.migrate")
          : t(`steps.${p as "restore" | "database" | "starting"}`),
      st: i < at ? "done" : i === at ? "now" : "",
    }))
  } else {
    steps = [{ label: t("steps.wait"), st: "now" }]
  }
  const sec = Math.max(0, Math.floor((now - since) / 1000))
  const elapsed = sec < 60 ? `${sec}s` : `${Math.floor(sec / 60)}m ${sec % 60}s`

  return (
    <div
      role="alertdialog"
      aria-live="polite"
      aria-label={
        h ? (updating ? t("updating") : t("starting")) : t("restarting")
      }
      className="fixed inset-0 z-[100] flex items-center justify-center bg-background/85 p-4 backdrop-blur-sm"
    >
      <div className="w-full max-w-[30rem] border bg-popover">
        <div className="flex items-center gap-1.5 border-b border-dashed px-3 py-2 text-xs text-muted-foreground">
          <i className="size-2 border" />
          <i className="size-2 border" />
          <i className="size-2 border" />
          <span className="ml-auto">pingtxt</span>
        </div>
        <div className="px-5 pt-5 pb-4">
          <div className="flex items-center gap-3">
            <svg
              viewBox="0 0 32 32"
              className="size-9 shrink-0"
              aria-hidden="true"
            >
              <rect width="32" height="32" fill="#111" />
              <path
                d="M6 10l6 6-6 6"
                stroke="#4ade80"
                strokeWidth="3"
                fill="none"
                strokeLinecap="square"
              />
              <rect
                className="cursor-blink"
                x="15"
                y="20"
                width="11"
                height="3"
                fill="#4ade80"
              />
            </svg>
            <div>
              <p className="text-[15px] font-semibold">
                {h
                  ? updating
                    ? t("updating")
                    : t("starting")
                  : t("restarting")}
              </p>
              <p className="text-muted-foreground">
                {updating ? (
                  <>
                    <b className="font-semibold text-foreground">
                      {h?.previous_version}
                    </b>{" "}
                    →{" "}
                    <b className="font-semibold text-foreground">
                      {h?.version}
                    </b>
                  </>
                ) : h ? (
                  t("hint")
                ) : (
                  t("unreachable")
                )}
              </p>
            </div>
          </div>

          <svg
            viewBox="0 0 500 48"
            preserveAspectRatio="none"
            className="mt-[18px] mb-3.5 block h-12 w-full overflow-visible"
            aria-hidden="true"
          >
            <path
              d={PULSE}
              className="fill-none stroke-border"
              strokeWidth="2"
              strokeLinejoin="round"
            />
            <path
              d={PULSE}
              pathLength={500}
              className={cn(
                "hub-beat fill-none",
                h ? "stroke-up text-up" : "stroke-warn text-warn"
              )}
              strokeWidth="2"
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          </svg>

          <ul>
            {steps.map((s) => (
              <li
                key={s.label}
                className={cn(
                  "flex gap-2.5",
                  s.st === "now" ? "text-foreground" : "text-muted-foreground"
                )}
              >
                <span
                  className={cn(
                    "w-[1ch] shrink-0 text-center",
                    s.st === "done" && "text-up",
                    s.st === "now" && "text-warn"
                  )}
                >
                  {s.st === "done" ? "✓" : s.st === "now" ? frame : "·"}
                </span>
                <span>{s.label}</span>
              </li>
            ))}
          </ul>

          <div className="mt-4 flex justify-between gap-3 border-t border-dashed pt-2.5 text-xs text-muted-foreground">
            <span>{elapsed}</span>
            <span>
              {t("waiting")}
              <span className="cursor-blink">_</span>
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}

/** Registers sw.js, which shows the starting page when the hub is down. */
export function ServiceWorker() {
  useEffect(() => {
    if (
      process.env.NODE_ENV !== "production" ||
      !("serviceWorker" in navigator)
    )
      return
    navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => {})
  }, [])
  return null
}
