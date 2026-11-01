"use client"

import { useRouter } from "next/navigation"
import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslations } from "use-intl"

import { StatusGlyph } from "@/components/status"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { useApi, type MonitorSummary, type Status } from "@/lib/api"
import { displayTarget } from "@/lib/format"
import { cn } from "@/lib/utils"

export interface CommandLink {
  href: string
  label: string
}

interface Item {
  key: string
  href: string
  label: string
  hint?: string
  status?: Status
}

/**
 * Quick jump menu (Ctrl+K or ⌘K): pages, actions and monitors, filtered by
 * what is typed. Arrow keys move, Enter opens.
 */
export function CommandMenu({
  open,
  onOpenChange,
  pages,
  actions,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  pages: CommandLink[]
  actions: CommandLink[]
}) {
  const router = useRouter()
  const t = useTranslations("commandMenu")
  const monitors = useApi<MonitorSummary[]>(open ? "/api/monitors" : null)
  const [q, setQ] = useState("")
  const [active, setActive] = useState(0)
  const listRef = useRef<HTMLUListElement>(null)

  // Start empty each time the menu opens.
  const [wasOpen, setWasOpen] = useState(open)
  if (wasOpen !== open) {
    setWasOpen(open)
    if (open) {
      setQ("")
      setActive(0)
    }
  }

  const items = useMemo(() => {
    const s = q.trim().toLowerCase()
    const match = (...xs: (string | undefined)[]) =>
      !s || xs.some((x) => x?.toLowerCase().includes(s))
    const out: { title: string; items: Item[] }[] = [
      {
        title: t("actions"),
        items: actions
          .filter((a) => match(a.label))
          .map((a) => ({ key: "a" + a.href, ...a })),
      },
      {
        title: t("monitors"),
        items: (monitors.data ?? [])
          .filter((m) => match(m.name, m.target, m.type))
          .slice(0, s ? 30 : 8)
          .map((m) => ({
            key: "m" + m.id,
            href: `/monitors/view/?id=${m.id}`,
            label: m.name,
            hint: m.type === "push" ? "push" : displayTarget(m.target),
            status: m.status,
          })),
      },
      {
        title: t("pages"),
        items: pages
          .filter((p) => match(p.label, p.href))
          .map((p) => ({ key: "p" + p.href, ...p })),
      },
    ]
    return out.filter((g) => g.items.length > 0)
  }, [q, monitors.data, pages, actions, t])
  const flat = items.flatMap((g) => g.items)
  const current = Math.min(active, Math.max(flat.length - 1, 0))

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${current}"]`)
      ?.scrollIntoView({ block: "nearest" })
  }, [current])

  function go(item: Item | undefined) {
    if (!item) return
    onOpenChange(false)
    router.push(item.href)
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setActive((current + 1) % Math.max(flat.length, 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActive((current - 1 + flat.length) % Math.max(flat.length, 1))
    } else if (e.key === "Enter") {
      e.preventDefault()
      go(flat[current])
    }
  }

  let index = -1
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="top-[15%] translate-y-0 gap-0 p-0 sm:max-w-lg"
      >
        <DialogTitle className="sr-only">{t("title")}</DialogTitle>
        <div className="flex items-center gap-2 border-b border-dashed px-3">
          <span className="text-primary">&gt;</span>
          <input
            autoFocus
            value={q}
            onChange={(e) => {
              setQ(e.target.value)
              setActive(0)
            }}
            onKeyDown={onKeyDown}
            placeholder={t("placeholder")}
            className="h-11 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            role="combobox"
            aria-expanded
            aria-controls="command-list"
            aria-activedescendant={flat[current] ? `cmd-${flat[current].key}` : undefined}
          />
        </div>
        <ul
          id="command-list"
          ref={listRef}
          role="listbox"
          className="max-h-80 overflow-y-auto py-1 text-sm"
        >
          {flat.length === 0 && (
            <li className="px-3 py-6 text-center text-xs text-muted-foreground">
              {t("empty")}
            </li>
          )}
          {items.map((g) => (
            <li key={g.title} role="presentation">
              <div className="px-3 pt-2 pb-1 text-[11px] text-muted-foreground uppercase">
                {g.title}
              </div>
              <ul role="presentation">
                {g.items.map((item) => {
                  index++
                  const i = index
                  return (
                    <li
                      key={item.key}
                      id={`cmd-${item.key}`}
                      data-index={i}
                      role="option"
                      aria-selected={i === current}
                      onMouseMove={() => i !== current && setActive(i)}
                      onClick={() => go(item)}
                      className={cn(
                        "flex cursor-pointer items-center gap-2 px-3 py-1.5",
                        i === current && "bg-accent"
                      )}
                    >
                      {item.status ? (
                        <StatusGlyph status={item.status} />
                      ) : (
                        <span className="text-muted-foreground">~/</span>
                      )}
                      <span className="truncate">{item.label}</span>
                      {item.hint && (
                        <span className="ml-auto truncate pl-2 text-xs text-muted-foreground">
                          {item.hint}
                        </span>
                      )}
                    </li>
                  )
                })}
              </ul>
            </li>
          ))}
        </ul>
        <div className="flex flex-wrap gap-x-3 gap-y-1 border-t border-dashed px-3 py-2 text-[11px] text-muted-foreground">
          {t.rich("keys", { kbd: (c) => <kbd className="border px-1">{c}</kbd> })}
        </div>
      </DialogContent>
    </Dialog>
  )
}
