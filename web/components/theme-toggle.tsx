"use client"

import { useTheme } from "next-themes"

import { Button } from "@/components/ui/button"
import { useMounted } from "@/hooks/use-now"

const order = ["system", "light", "dark"] as const

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const mounted = useMounted()
  const cur = mounted ? (theme ?? "system") : "system"
  const next = order[(order.indexOf(cur as (typeof order)[number]) + 1) % order.length]
  return (
    <Button variant="ghost" size="sm" onClick={() => setTheme(next)} title="toggle theme (d)">
      [{cur}]
    </Button>
  )
}
