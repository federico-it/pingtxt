import { useEffect, useRef } from "react"

/** True when the key press goes to a text field, where shortcuts must not fire. */
export function isTypingTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false
  return (
    target.isContentEditable ||
    target.tagName === "INPUT" ||
    target.tagName === "TEXTAREA" ||
    target.tagName === "SELECT" ||
    target.getAttribute("role") === "combobox"
  )
}

/**
 * Runs fn when a single key is pressed outside text fields and dialogs,
 * without modifier keys (Shift is allowed, for keys like "?").
 */
export function useHotkey(key: string, fn: () => void, enabled = true) {
  const ref = useRef(fn)
  useEffect(() => {
    ref.current = fn
  })
  useEffect(() => {
    if (!enabled) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented || e.repeat || e.metaKey || e.ctrlKey || e.altKey) return
      if (e.key !== key || isTypingTarget(e.target)) return
      // Leave keys alone while a dialog or menu is open.
      if (document.querySelector("[role=dialog],[role=alertdialog],[role=menu]")) return
      e.preventDefault()
      ref.current()
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [key, enabled])
}
