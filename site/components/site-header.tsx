"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useState } from "react"

import { LANG_KEY, useLang, useT } from "@/components/lang"
import { StatusLink } from "@/components/status-dot"
import { ThemeToggle } from "@/components/theme"
import { localePath, splitPath, type Lang } from "@/lib/i18n"

export type NavItem = { href: string; label: string }

const item = "px-1.5 py-0.5 transition-colors hover:bg-accent hover:text-foreground"

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

// On an English page, a browser set to Italian gets a one-line hint, until a
// language is picked or the hint is closed.
function ItalianHint() {
  const lang = useLang()
  const [other, href] = useOther()
  const [show, setShow] = useState(false)
  useEffect(() => {
    if (lang !== "en") return
    let saved: string | null = null
    try {
      saved = localStorage.getItem(LANG_KEY)
    } catch {}
    const it = (navigator.languages ?? [navigator.language]).some((l) => l?.toLowerCase().startsWith("it"))
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (!saved && it) setShow(true)
  }, [lang])
  if (!show || other !== "it") return null
  return (
    <div lang="it" className="flex items-center gap-2 border-t border-dashed py-1.5 text-xs text-muted-foreground">
      <span className="text-up">$</span>
      <span>
        questa pagina è anche in italiano:{" "}
        <a href={href} hrefLang="it" onClick={(e) => go(e, "it")} className="text-foreground underline underline-offset-4">
          leggi in italiano
        </a>
      </span>
      <button
        type="button"
        onClick={() => {
          remember("en")
          setShow(false)
        }}
        aria-label="chiudi"
        className={`${item} ml-auto`}
      >
        [×]
      </button>
    </div>
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
    <header className="sticky top-0 z-30 -mx-4 border-b border-dashed bg-background/90 px-4 backdrop-blur sm:-mx-6 sm:px-6">
      <div className="flex h-12 items-center gap-6">
        <Link href={localePath(lang, "/")} className="font-semibold tracking-tight">
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
      {switcher && <ItalianHint />}
    </header>
  )
}
