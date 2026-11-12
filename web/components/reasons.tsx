"use client"

import { useTranslations } from "use-intl"

import type { Reason } from "@/lib/api"
import { cn } from "@/lib/utils"

// parseCause splits the one-line cause of an incident ("loc1, loc2: message;
// loc3: other message") into its problems and merges the locations that
// report the same message. Older incidents list every location on its own,
// so ten agents reading the same page become one line here too. A part
// without "location: " belongs to the message before it (a message that
// contains "; ").
export function parseCause(cause: string): Reason[] {
  const out: Reason[] = []
  const at = new Map<string, number>()
  let last: Reason | null = null
  for (const part of cause.split("; ")) {
    const i = part.indexOf(": ")
    if (i <= 0) {
      if (last) {
        last.message += "; " + part
        continue
      }
      if (part.trim()) out.push((last = { kind: "down", message: part, locations: [] }))
      continue
    }
    const locs = part.slice(0, i).split(", ")
    const message = part.slice(i + 2)
    const known = at.get(message)
    if (known !== undefined) {
      out[known].locations.push(...locs)
      last = out[known]
      continue
    }
    at.set(message, out.length)
    out.push((last = { kind: "down", message, locations: locs }))
  }
  return out
}

// ReasonList shows each problem once, with the locations that report it.
// With total (the monitor's location count) a problem seen everywhere says
// "all locations"; with compact, more than two locations become a count.
export function ReasonList({
  reasons,
  total = 0,
  compact = false,
  className,
}: {
  reasons: Reason[]
  total?: number
  compact?: boolean
  className?: string
}) {
  const t = useTranslations("reasons")
  if (!reasons.length) return null
  return (
    <ul className={cn("grid gap-1", className)}>
      {reasons.map((r, i) => {
        const all = total > 1 && r.locations.length >= total
        const many = compact && r.locations.length > 2
        return (
          <li key={i} className="min-w-0 [overflow-wrap:anywhere]">
            <span className="text-foreground">{r.message || t("noMessage")}</span>
            {r.locations.length > 0 && (
              <span className="text-muted-foreground" title={many || all ? r.locations.join("\n") : undefined}>
                {" — "}
                {all
                  ? t("allLocations")
                  : many
                    ? t("locations", { count: r.locations.length })
                    : r.locations.join(", ")}
              </span>
            )}
          </li>
        )
      })}
    </ul>
  )
}

// useReasonLine gives a one-line summary for the dashboard: the first problem
// and how many more there are.
export function useReasonLine() {
  const t = useTranslations("reasons")
  return (reasons: Reason[]) => {
    if (!reasons.length) return ""
    const first = reasons[0].message
    return reasons.length > 1 ? t("andMore", { first, count: reasons.length - 1 }) : first
  }
}
