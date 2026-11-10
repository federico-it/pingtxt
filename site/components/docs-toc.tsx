"use client"

import { useEffect, useState } from "react"

import { useT } from "@/components/lang"

export type TocItem = { id: string; text: string; depth: number }

// "On this page", right of the content on wide screens. The section being
// read is the last heading above the top third of the window.
export function DocsToc({ items }: { items: TocItem[] }) {
  const t = useT()
  const [active, setActive] = useState("")
  useEffect(() => {
    const els = items.map((i) => document.getElementById(i.id)).filter((e): e is HTMLElement => !!e)
    function update() {
      const line = window.innerHeight / 3
      let cur = ""
      for (const el of els) {
        if (el.getBoundingClientRect().top <= line) cur = el.id
        else break
      }
      setActive(cur)
    }
    update()
    window.addEventListener("scroll", update, { passive: true })
    window.addEventListener("resize", update)
    return () => {
      window.removeEventListener("scroll", update)
      window.removeEventListener("resize", update)
    }
  }, [items])
  if (!items.length) return null
  return (
    <nav
      aria-label={t("on this page", "in questa pagina")}
      className="hidden xl:sticky xl:top-12 xl:block xl:max-h-[calc(100svh-3rem)] xl:self-start xl:overflow-y-auto xl:py-8"
    >
      <div className="mb-3 text-[10.5px] font-semibold uppercase tracking-widest text-muted-foreground">
        {t("on this page", "in questa pagina")}
      </div>
      <ul className="border-l border-dashed text-[12px]">
        {items.map((i) => (
          <li key={i.id}>
            <a
              href={`#${i.id}`}
              className={`-ml-px block border-l py-1 transition-colors ${i.depth > 2 ? "pl-6" : "pl-3"} ${
                active === i.id
                  ? "border-up text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
              }`}
            >
              {i.text}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  )
}
