import { useSyncExternalStore } from "react"
import { toast } from "sonner"

import { api } from "@/lib/api"

// Monitors deleted with "undo": they disappear right away and the delete is
// sent when the undo time runs out (or when the page is closed).

const undoMs = 8000

let pending = new Set<number>()
const listeners = new Set<() => void>()
const timers = new Map<string, { ids: number[]; timer: ReturnType<typeof setTimeout> }>()

function emit() {
  pending = new Set(pending)
  for (const l of listeners) l()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

const empty = new Set<number>()

/** Ids of monitors waiting to be deleted: hide them. */
export function usePendingDeletes() {
  return useSyncExternalStore(subscribe, () => pending, () => empty)
}

function send(ids: number[]) {
  return ids.length === 1
    ? api(`/api/monitors/${ids[0]}`, { method: "DELETE" })
    : api("/api/monitors/bulk", { method: "POST", body: { ids, action: "delete" } })
}

/**
 * Deletes monitors after a short delay, with an undo button in a toast.
 * onDone runs after the delete went through.
 */
export function deleteWithUndo(
  ids: number[],
  text: { done: string; undo: string; undone: string; error: string },
  onDone?: () => void
) {
  if (ids.length === 0) return
  const key = ids.join(",") + ":" + Date.now()
  for (const id of ids) pending.add(id)
  emit()
  const finish = async () => {
    timers.delete(key)
    try {
      await send(ids)
      onDone?.()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : text.error)
    } finally {
      for (const id of ids) pending.delete(id)
      emit()
    }
  }
  const timer = setTimeout(finish, undoMs)
  timers.set(key, { ids, timer })
  toast.success(text.done, {
    duration: undoMs,
    action: {
      label: text.undo,
      onClick: () => {
        const t = timers.get(key)
        if (!t) return
        clearTimeout(t.timer)
        timers.delete(key)
        for (const id of ids) pending.delete(id)
        emit()
        toast(text.undone)
      },
    },
  })
}

// Leaving the page sends the deletes that are still waiting.
if (typeof window !== "undefined") {
  window.addEventListener("pagehide", () => {
    for (const [key, t] of timers) {
      clearTimeout(t.timer)
      timers.delete(key)
      const body = JSON.stringify({ ids: t.ids, action: "delete" })
      void fetch("/api/monitors/bulk", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body,
        keepalive: true,
      })
    }
  })
}
