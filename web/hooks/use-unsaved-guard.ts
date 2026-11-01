import { useEffect, useRef } from "react"

/**
 * Asks before leaving a form with unsaved changes: on reload, tab close and
 * clicks on links inside the app. confirmLeave() is for buttons that leave
 * the page in code (cancel, back).
 */
export function useUnsavedGuard(dirty: boolean, message: string) {
  const state = useRef({ dirty, message, released: false })
  useEffect(() => {
    state.current.dirty = dirty
    state.current.message = message
  })
  const blocked = () => state.current.dirty && !state.current.released

  useEffect(() => {
    function onBeforeUnload(e: BeforeUnloadEvent) {
      if (!blocked()) return
      e.preventDefault()
      e.returnValue = ""
    }
    function onClick(e: MouseEvent) {
      if (!blocked() || e.defaultPrevented || e.button !== 0) return
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return // new tab or window
      const a = (e.target as Element | null)?.closest?.("a[href]")
      if (!(a instanceof HTMLAnchorElement)) return
      if (a.target && a.target !== "_self") return
      const url = new URL(a.href, window.location.href)
      if (url.origin !== window.location.origin) return
      if (url.pathname === window.location.pathname && url.search === window.location.search) return
      if (!window.confirm(state.current.message)) {
        e.preventDefault()
        e.stopPropagation()
      }
    }
    window.addEventListener("beforeunload", onBeforeUnload)
    // Capture phase: runs before Next's Link handler starts the navigation.
    document.addEventListener("click", onClick, true)
    return () => {
      window.removeEventListener("beforeunload", onBeforeUnload)
      document.removeEventListener("click", onClick, true)
    }
  }, [])

  return {
    confirmLeave: () => !blocked() || window.confirm(state.current.message),
    /** Turns the guard off, e.g. right after a successful save. */
    release: () => {
      state.current.released = true
    },
  }
}
