import { useSyncExternalStore } from "react"

// Monitors pinned to the top of the dashboard, remembered in this browser
// (like folded groups). Other tabs follow through the storage event.

const key = "pingtxt.pinned"
const empty: ReadonlySet<number> = new Set()
let pins: ReadonlySet<number> | null = null
const listeners = new Set<() => void>()

function read(): ReadonlySet<number> {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? "[]")
    return new Set(Array.isArray(v) ? v.map(Number).filter((n) => n > 0) : [])
  } catch {
    return new Set()
  }
}

function snapshot() {
  pins ??= read()
  return pins
}

function subscribe(l: () => void) {
  listeners.add(l)
  const onStorage = (e: StorageEvent) => {
    if (e.key !== key) return
    pins = read()
    l()
  }
  window.addEventListener("storage", onStorage)
  return () => {
    listeners.delete(l)
    window.removeEventListener("storage", onStorage)
  }
}

/** Ids of the pinned monitors. */
export function usePins() {
  return useSyncExternalStore(subscribe, snapshot, () => empty)
}

export function togglePin(id: number) {
  const next = new Set(snapshot())
  if (next.has(id)) next.delete(id)
  else next.add(id)
  pins = next
  try {
    localStorage.setItem(key, JSON.stringify([...next]))
  } catch {}
  for (const l of listeners) l()
}
