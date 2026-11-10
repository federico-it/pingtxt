"use client"

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useMemo, useRef, useState } from "react"

import { useLang, useT } from "@/components/lang"
import type { SearchEntry } from "@/lib/docs"

export type NavGroup = { label: string; items: { href: string; label: string }[] }

const row = "block border-l-2 px-3 py-1.5 transition-colors"

// The docs sidebar: title, search box and the pages in groups. Below lg it
// folds behind a [contents] button, like the site menu.
export function DocsNav({ title, groups }: { title: string; groups: NavGroup[] }) {
  const t = useT()
  const pathname = usePathname()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const results = useResults(query)
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setOpen(false), [pathname])
  const here = (href: string) => pathname === href || pathname === href.replace(/\/$/, "")

  return (
    <aside className="border-dashed lg:sticky lg:top-12 lg:max-h-[calc(100svh-3rem)] lg:self-start lg:overflow-y-auto lg:border-r lg:py-8 lg:pr-6">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-controls="docs-nav"
        className="border bg-background px-3 py-1.5 text-[11px] uppercase tracking-wider shadow-[3px_3px_0_0_var(--color-up)] transition-transform active:translate-x-px active:translate-y-px lg:hidden"
      >
        {open ? t("close contents", "chiudi indice") : t("contents", "indice")}
      </button>
      <div id="docs-nav" className={`${open ? "block" : "hidden"} mt-6 lg:mt-0 lg:block`}>
        <div className="text-[11px] font-semibold uppercase tracking-widest text-up">{"// "}{t("the manual", "il manuale")}</div>
        <div className="mt-1 text-base font-bold">{title}</div>
        <Search query={query} setQuery={setQuery} first={results?.[0]?.href} />
        <div className="-mr-6 mt-6 border-t border-dashed max-lg:mr-0" />
        {query.trim() ? (
          <Results query={query} results={results} />
        ) : (
          <nav className="mt-4">
            {groups.map((g, i) => (
              <div key={i} className={i ? "mt-4 border-t border-dashed pt-4" : undefined}>
                {g.label && (
                  <div className="mb-1.5 px-3 text-[10.5px] font-semibold uppercase tracking-widest text-muted-foreground">
                    {g.label}
                  </div>
                )}
                <ul>
                  {g.items.map((it) => {
                    const active = here(it.href)
                    return (
                      <li key={it.href}>
                        <Link
                          href={it.href}
                          aria-current={active ? "page" : undefined}
                          className={
                            active
                              ? `${row} border-up bg-up/15 font-semibold text-foreground`
                              : `${row} border-transparent text-muted-foreground hover:bg-accent hover:text-foreground`
                          }
                        >
                          {it.label}
                        </Link>
                      </li>
                    )
                  })}
                </ul>
              </div>
            ))}
          </nav>
        )}
      </div>
    </aside>
  )
}

// The index is fetched when the first query is typed. "/" focuses the box.
let cache: Promise<SearchEntry[]> | null = null

function useIndex(lang: string, want: boolean) {
  const [index, setIndex] = useState<SearchEntry[] | null>(null)
  useEffect(() => {
    if (!want || index) return
    cache ??= fetch(`/docs-search.${lang}.json`).then((r) => r.json())
    cache.then(setIndex).catch(() => (cache = null))
  }, [lang, want, index])
  return index
}

// Pages and sections where every word of the query appears, best first: a
// word in the section title counts more than one in the page title or text.
function useResults(query: string) {
  const lang = useLang()
  const index = useIndex(lang, query.trim() !== "")
  return useMemo(() => {
    if (!index) return null
    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    const scored: [number, SearchEntry][] = []
    for (const e of index) {
      const page = e.page.toLowerCase()
      const title = e.title.toLowerCase()
      const text = e.text.toLowerCase()
      let score = 0
      for (const w of words) {
        if (title.includes(w)) score += 10
        else if (page.includes(w)) score += 4
        else if (text.includes(w)) score += 1
        else {
          score = 0
          break
        }
      }
      if (score) scored.push([score, e])
    }
    return scored
      .sort((a, b) => b[0] - a[0])
      .slice(0, 12)
      .map(([, e]) => e)
  }, [index, query])
}

function Search({ query, setQuery, first }: { query: string; setQuery: (q: string) => void; first?: string }) {
  const t = useT()
  const router = useRouter()
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return
      const el = e.target
      if (el instanceof HTMLElement && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName))) return
      if (!ref.current?.offsetParent) return
      e.preventDefault()
      ref.current.focus()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
  return (
    <div className="relative mt-4">
      <input
        ref={ref}
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") setQuery("")
          if (e.key === "Enter" && first) {
            router.push(first)
            setQuery("")
            ref.current?.blur()
          }
        }}
        placeholder={t("search the manual…", "cerca nel manuale…")}
        aria-label={t("search the manual", "cerca nel manuale")}
        className="w-full border bg-background px-3 py-2 text-[13px] outline-none placeholder:text-muted-foreground focus:border-foreground"
      />
      <kbd hidden={query !== ""} className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 border px-1 text-[11px] text-muted-foreground max-lg:hidden">
        /
      </kbd>
    </div>
  )
}

function Results({ query, results }: { query: string; results: SearchEntry[] | null }) {
  const t = useT()
  if (!results) return <p className="mt-4 px-3 text-muted-foreground">{t("loading…", "caricamento…")}</p>
  if (!results.length) return <p className="mt-4 px-3 text-muted-foreground">{t("no results", "nessun risultato")}</p>
  const first = query.toLowerCase().split(/\s+/).find(Boolean) ?? ""
  return (
    <ul className="mt-4 space-y-1" aria-label={t("search results", "risultati della ricerca")}>
      {results.map((e, i) => (
        <li key={e.href}>
          <Link
            href={e.href}
            className={`${row} ${i ? "border-transparent" : "border-up bg-up/10"} hover:bg-accent`}
          >
            <span className="block text-[11px] text-muted-foreground">{e.page === e.title ? "§" : e.page}</span>
            <span className="block font-semibold text-foreground">{e.title}</span>
            {e.text && <span className="line-clamp-2 text-[12px] text-muted-foreground">{snippet(e.text, first)}</span>}
          </Link>
        </li>
      ))}
    </ul>
  )
}

function snippet(text: string, word: string) {
  const i = word ? text.toLowerCase().indexOf(word) : -1
  if (i < 40) return text.slice(0, 140)
  return "…" + text.slice(i - 30, i + 110)
}
