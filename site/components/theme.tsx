"use client"

import { ThemeProvider as NextThemes, useTheme } from "next-themes"
import { useEffect, useState } from "react"

import { useT } from "@/components/lang"

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  return (
    <NextThemes attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
      <Hotkey />
      {children}
    </NextThemes>
  )
}

// "d" toggles light/dark, like in the app.
function Hotkey() {
  const { resolvedTheme, setTheme } = useTheme()
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.defaultPrevented || e.repeat || e.metaKey || e.ctrlKey || e.altKey) return
      if (e.key.toLowerCase() !== "d") return
      const t = e.target
      if (t instanceof HTMLElement && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return
      setTheme(resolvedTheme === "dark" ? "light" : "dark")
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [resolvedTheme, setTheme])
  return null
}

const order = ["system", "light", "dark"] as const

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const t = useT()
  const [mounted, setMounted] = useState(false)
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setMounted(true), [])
  const cur = (mounted ? (theme ?? "system") : "system") as (typeof order)[number]
  const next = order[(order.indexOf(cur) + 1) % order.length]
  return (
    <button
      type="button"
      onClick={() => setTheme(next)}
      title={t("toggle theme (d)", "cambia tema (d)")}
      className="px-1.5 py-0.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      [{cur}]
    </button>
  )
}
