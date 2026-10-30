import { useSyncExternalStore } from "react"

function subscribe(cb: () => void) {
  const t = setInterval(cb, 1000)
  return () => clearInterval(t)
}

/** Current time in ms, rounded to the second and refreshed every second. */
export function useNow() {
  return useSyncExternalStore(
    subscribe,
    () => Math.floor(Date.now() / 1000) * 1000,
    () => 0
  )
}

function subscribeMinute(cb: () => void) {
  const t = setInterval(cb, 60_000)
  return () => clearInterval(t)
}

/** Current time in ms, rounded to the minute: for slow-changing labels in long lists. */
export function useMinute() {
  return useSyncExternalStore(
    subscribeMinute,
    () => Math.floor(Date.now() / 60_000) * 60_000,
    () => 0
  )
}

const noop = () => () => {}

/** True after hydration on the client. */
export function useMounted() {
  return useSyncExternalStore(
    noop,
    () => true,
    () => false
  )
}
