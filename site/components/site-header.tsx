"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useState } from "react"

import { LANG_KEY, useLang, useT } from "@/components/lang"
import { StatusLink } from "@/components/status-dot"
import { WRAP } from "@/components/ui"
import { ThemeToggle } from "@/components/theme"
import { localePath, splitPath, type Lang } from "@/lib/i18n"

export type NavItem = { href: string; label: string; external?: boolean }

const item = "px-1.5 py-0.5 transition-colors hover:bg-accent hover:text-foreground"
const navItem = "px-2 py-1 text-[11.5px] font-semibold uppercase tracking-widest transition-colors hover:bg-up/15"

function remember(lang: Lang) {
  try {
    localStorage.setItem(LANG_KEY, lang)
  } catch {}
}

// The same page in the other language, keeping the #section.
function useOther(): [Lang, string] {
  const lang = useLang()
  const [, path] = splitPath(usePathname() || "/")
  const other: Lang = lang === "en" ? "it" : "en"
  return [other, localePath(other, path)]
}

function go(e: React.MouseEvent<HTMLAnchorElement>, lang: Lang) {
  remember(lang)
  e.currentTarget.href += window.location.hash
}

// [it] / [en] next to the theme toggle.
function LangSwitch() {
  const [other, href] = useOther()
  return (
    <a
      href={href}
      hrefLang={other}
      lang={other}
      onClick={(e) => go(e, other)}
      title={other === "it" ? "leggi in italiano" : "read in english"}
      className={`${item} text-muted-foreground`}
    >
      [{other}]
    </a>
  )
}

// One row from md up. Below md: logo, language, theme and a [≡] button that
// opens the nav as a panel under the bar (closed again on navigation).
export function SiteHeader({ nav, switcher = true }: { nav: NavItem[]; switcher?: boolean }) {
  const [open, setOpen] = useState(false)
  const pathname = usePathname()
  const lang = useLang()
  const t = useT()
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setOpen(false), [pathname])

  return (
    <header className="sticky top-0 z-30 border-b border-edge bg-background/90 backdrop-blur">
      <div className={`${WRAP} flex h-12 items-center gap-6`}>
        <Link href={localePath(lang, "/")} className="text-[15px] font-bold tracking-tight">
          <span className="text-up">&gt;</span> pingtxt<span className="cursor-blink text-up">_</span>
        </Link>
        <nav className="ml-auto hidden items-center gap-1 md:flex">
          {nav.map((n) => (
            <Link key={n.href} href={n.href} className={navItem}>
              {n.label}
              {n.external && <span className="ml-0.5 text-muted-foreground">↗</span>}
            </Link>
          ))}
          <StatusLink className={navItem} />
        </nav>
        <div className="ml-auto flex items-center gap-1 md:ml-0 md:border-l md:border-dashed md:pl-3">
          {switcher && <LangSwitch />}
          <ThemeToggle />
          <button
            type="button"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
            aria-controls="site-menu"
            aria-label={open ? t("close menu", "chiudi menu") : t("open menu", "apri menu")}
            className={`${item} text-muted-foreground md:hidden`}
          >
            {open ? "[×]" : "[≡]"}
          </button>
        </div>
      </div>
      {open && (
        <nav id="site-menu" className={`${WRAP} space-y-2 border-t border-dashed py-3 md:hidden`}>
          <div className="text-xs text-muted-foreground">
            <span className="text-up">$</span> ls ~/
          </div>
          <div className="grid grid-cols-2 gap-1">
            {nav.map((n) => (
              <Link key={n.href} href={n.href} onClick={() => setOpen(false)} className={`${navItem} py-2`}>
                {n.label}
                {n.external && <span className="ml-0.5 text-muted-foreground">↗</span>}
              </Link>
            ))}
            <StatusLink className={`${navItem} py-2`} />
          </div>
        </nav>
      )}
    </header>
  )
}
