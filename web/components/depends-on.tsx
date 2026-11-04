"use client"

import Link from "next/link"
import { XIcon } from "lucide-react"
import { useMemo } from "react"
import { useTranslations } from "use-intl"

import { StatusGlyph } from "@/components/status"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useApi, type MonitorSummary } from "@/lib/api"

/**
 * Picks the monitors a monitor depends on. Monitors that already depend on
 * this one (directly or further down) are left out: the hub refuses loops.
 */
export function DependsOnPicker({
  id,
  value,
  onChange,
}: {
  /** the monitor being edited, undefined while creating it */
  id?: number
  value: number[]
  onChange: (ids: number[]) => void
}) {
  const t = useTranslations("dependencies")
  const monitors = useApi<MonitorSummary[]>("/api/monitors", { refreshInterval: 0 })
  const list = useMemo(() => monitors.data ?? [], [monitors.data])
  const byId = useMemo(() => new Map(list.map((m) => [m.id, m])), [list])

  // Monitors that depend on this one, which cannot become its parents.
  const below = useMemo(() => {
    const out = new Set<number>()
    if (id === undefined) return out
    let grew = true
    out.add(id)
    while (grew) {
      grew = false
      for (const m of list) {
        if (!out.has(m.id) && (m.depends_on ?? []).some((p) => out.has(p))) {
          out.add(m.id)
          grew = true
        }
      }
    }
    return out
  }, [id, list])

  const available = list.filter((m) => !below.has(m.id) && !value.includes(m.id))
  const items = [
    { value: "0", label: t("add") },
    ...available.map((m) => ({ value: String(m.id), label: m.name })),
  ]

  return (
    <div className="grid gap-2">
      {value.length > 0 && (
        <div className="border-t border-dashed">
          {value.map((p) => {
            const m = byId.get(p)
            return (
              <div key={p} className="flex items-center gap-2 border-b border-dashed px-2 py-1.5">
                {m && <StatusGlyph status={m.status} />}
                <Link href={`/monitors/view/?id=${p}`} className="min-w-0 flex-1 truncate hover:underline">
                  {m?.name ?? `#${p}`}
                </Link>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label={t("remove", { name: m?.name ?? `#${p}` })}
                  onClick={() => onChange(value.filter((x) => x !== p))}
                >
                  <XIcon />
                </Button>
              </div>
            )
          })}
        </div>
      )}
      <Select
        items={items}
        value="0"
        onValueChange={(v) => {
          const n = Number(v ?? 0)
          if (n > 0 && !value.includes(n)) onChange([...value, n])
        }}
      >
        <SelectTrigger className="w-full sm:w-80" disabled={available.length === 0}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((i) => (
            <SelectItem key={i.value} value={i.value}>
              {i.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}
