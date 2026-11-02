"use client"

import Link from "next/link"
import { useTranslations } from "use-intl"

import { useMinute } from "@/hooks/use-now"
import { useApi, type Overview } from "@/lib/api"
import { duration } from "@/lib/format"

/** Banner with the agents offline longer than the grace period: their
 * checks are not running. */
export function AgentsOffline() {
  const t = useTranslations("agentsOffline")
  const { data } = useApi<Overview>("/api/overview")
  const now = useMinute()
  const lost = data?.agents_lost ?? []
  if (lost.length === 0) return null
  return (
    <div role="status" className="mb-4 flex flex-wrap items-baseline gap-x-2 gap-y-1 border border-down/40 bg-down/5 px-3 py-2 text-xs">
      <span className="text-down">● {t("title", { count: lost.length })}</span>
      <span>
        {lost.map((a, i) => (
          <span key={a.id}>
            {i > 0 && ", "}
            {a.name}
            {a.offline_since > 0 && <span className="text-muted-foreground"> ({duration(now - a.offline_since)})</span>}
          </span>
        ))}
      </span>
      <span className="text-muted-foreground">· {t("text", { count: lost.length })}</span>
      <Link href="/agents/" className="ml-auto underline underline-offset-2 hover:text-foreground">
        {t("open")}
      </Link>
    </div>
  )
}
