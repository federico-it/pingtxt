"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useState } from "react"

import { StatusLink } from "@/components/status-dot"
import { ThemeToggle } from "@/components/theme"

export type NavItem = { href: string; label: string }

const item = "px-1.5 py-0.5 transition-colors hover:bg-accent hover:text-foreground"

// One row from md up. Below md: logo, theme and a [≡] button that opens the
// nav as a panel under the bar (closed again on navigation).
export function SiteHeader({ nav }: { nav: NavItem[] }) {
  const [open, setOpen] = useState(false)
  const pathname = usePathname()
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setOpen(false), [pathname])

  return (
    <header className="sticky top-0 z-30 -mx-4 border-b border-dashed bg-background/90 px-4 backdrop-blur sm:-mx-6 sm:px-6">
      <div className="flex h-12 items-center gap-6">
        <Link href="/" className="font-semibold tracking-tight">
          <span className="text-up">&gt;</span> pingtxt<span className="cursor-blink text-up">_</span>
        </Link>
        <nav className="hidden items-center gap-1 text-muted-foreground md:flex">
          {nav.map((n) => (
            <Link key={n.href} href={n.href} className={item}>
              {n.label}
            </Link>
          ))}
          <StatusLink className={item} />
        </nav>
        <div className="ml-auto flex items-center gap-1">
          <ThemeToggle />
          <button
            type="button"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
            aria-controls="site-menu"
            aria-label={open ? "close menu" : "open menu"}
            className={`${item} text-muted-foreground md:hidden`}
          >
            {open ? "[×]" : "[≡]"}
          </button>
        </div>
      </div>
      {open && (
        <nav id="site-menu" className="space-y-2 border-t border-dashed py-3 md:hidden">
          <div className="text-xs text-muted-foreground">
            <span className="text-up">$</span> ls ~/
          </div>
          <div className="grid grid-cols-2 gap-1 text-muted-foreground">
            {nav.map((n) => (
              <Link key={n.href} href={n.href} onClick={() => setOpen(false)} className={`${item} py-1.5`}>
                {n.label}
              </Link>
            ))}
            <StatusLink className={`${item} py-1.5`} />
          </div>
        </nav>
      )}
    </header>
  )
}
