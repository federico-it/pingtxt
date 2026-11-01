import { useSyncExternalStore } from "react"

// Desktop notifications from the open hub tab: on in this browser only, kept
// in localStorage. The browser's permission is asked when turning them on.

const key = "pingtxt.browserNotify"
const listeners = new Set<() => void>()

export type NotifyState = "unsupported" | "denied" | "off" | "on"

function supported() {
  return typeof window !== "undefined" && "Notification" in window
}

function state(): NotifyState {
  if (!supported()) return "unsupported"
  if (Notification.permission === "denied") return "denied"
  let on = false
  try {
    on = localStorage.getItem(key) === "1"
  } catch {}
  return on && Notification.permission === "granted" ? "on" : "off"
}

function subscribe(l: () => void) {
  listeners.add(l)
  const onStorage = (e: StorageEvent) => e.key === key && l()
  window.addEventListener("storage", onStorage)
  return () => {
    listeners.delete(l)
    window.removeEventListener("storage", onStorage)
  }
}

function emit() {
  for (const l of listeners) l()
}

export function useBrowserNotify(): NotifyState {
  return useSyncExternalStore(subscribe, state, () => "off")
}

/** Turns notifications on (asking for permission) or off. */
export async function setBrowserNotify(on: boolean): Promise<NotifyState> {
  if (!supported()) return "unsupported"
  if (on && Notification.permission !== "granted") {
    await Notification.requestPermission()
  }
  try {
    if (on && Notification.permission === "granted") localStorage.setItem(key, "1")
    else localStorage.removeItem(key)
  } catch {}
  emit()
  return state()
}

/**
 * Shows a notification. The tag makes several open tabs show it once.
 * Mobile browsers only allow notifications from the service worker.
 */
export async function notify(title: string, opts: { body: string; tag: string; onClick?: () => void }) {
  if (state() !== "on") return
  const options = { body: opts.body, tag: opts.tag, icon: "/icon.svg" }
  try {
    const n = new Notification(title, options)
    n.onclick = () => {
      window.focus()
      opts.onClick?.()
      n.close()
    }
  } catch {
    try {
      const reg = await navigator.serviceWorker?.getRegistration()
      await reg?.showNotification(title, options)
    } catch {}
  }
}
