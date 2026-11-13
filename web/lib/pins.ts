import { useEffect, useMemo } from "react"
import { mutate } from "swr"

import { api, useApi } from "@/lib/api"

// Monitors pinned to the top of the dashboard. They belong to the account
// (GET /api/account/pins), so every browser and the app show the same ones.
// Older versions kept them in this browser only: those are sent to the hub
// once and then forgotten here.

const path = "/api/account/pins"
const legacyKey = "pingtxt.pinned"
const empty: ReadonlySet<number> = new Set()

export interface MonitorPins {
  monitor_ids: number[]
}

let imported = false

/** Sends the pins an older version kept in this browser, then drops them. */
async function importLegacy(current: number[]) {
  if (imported) return
  imported = true
  let local: number[] = []
  try {
    const v = JSON.parse(localStorage.getItem(legacyKey) ?? "[]")
    local = Array.isArray(v) ? v.map(Number).filter((n) => n > 0) : []
  } catch {
    return
  }
  if (!local.length) return
  const missing = local.filter((id) => !current.includes(id))
  try {
    if (missing.length) {
      const res = await api<MonitorPins>(path, {
        method: "PUT",
        body: { monitor_ids: [...current, ...missing] },
      })
      await mutate(path, res, { revalidate: false })
    }
    localStorage.removeItem(legacyKey)
  } catch {
    imported = false // try again on the next load
  }
}

/** Ids of the pinned monitors. */
export function usePins(): ReadonlySet<number> {
  const { data } = useApi<MonitorPins>(path, { refreshInterval: 30_000 })
  useEffect(() => {
    if (data) void importLegacy(data.monitor_ids)
  }, [data])
  return useMemo(() => (data ? new Set(data.monitor_ids) : empty), [data])
}

/** Pins or unpins a monitor; the star changes at once. */
export function togglePin(id: number, pinned: boolean) {
  const method = pinned ? "DELETE" : "POST"
  void mutate<MonitorPins>(
    path,
    api<MonitorPins>(`${path}/${id}`, { method }),
    {
      optimisticData: (cur) => {
        const ids = cur?.monitor_ids ?? []
        return {
          monitor_ids: pinned
            ? ids.filter((x) => x !== id)
            : ids.includes(id)
              ? ids
              : [...ids, id],
        }
      },
      rollbackOnError: true,
      populateCache: true,
      revalidate: false,
    }
  ).catch(() => {})
}
