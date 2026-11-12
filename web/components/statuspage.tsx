"use client"

import { useEffect, useState } from "react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"

/** What the hub found at a URL: POST /api/monitors/statuspage. */
export interface StatuspageInfo {
  statuspage: boolean
  url?: string
  name?: string
  indicator?: string
  description?: string
  components?: StatuspageComponent[]
  error?: string
}

export interface StatuspageComponent {
  id: string
  name: string
  group?: string
  status: string
}

/** Whether a target looks like an http(s) URL worth asking about. */
function lookable(target: string) {
  try {
    const u = new URL(target.trim())
    return (
      (u.protocol === "https:" || u.protocol === "http:") &&
      u.hostname.includes(".")
    )
  } catch {
    return false
  }
}

/**
 * Asks the hub, a moment after the user stops typing, whether the target is
 * an Atlassian Statuspage page. Null while unknown or when disabled.
 */
export function useStatuspage(
  target: string,
  ignoreTLS: boolean,
  enabled: boolean
) {
  const [info, setInfo] = useState<{
    key: string
    info: StatuspageInfo
  } | null>(null)
  const [loading, setLoading] = useState(false)
  const key = `${target.trim()}|${ignoreTLS}`
  const active = enabled && lookable(target)

  useEffect(() => {
    if (!active) return
    let stale = false
    const timer = setTimeout(() => {
      setLoading(true)
      api<StatuspageInfo>("/api/monitors/statuspage", {
        method: "POST",
        body: { target: target.trim(), ignore_tls: ignoreTLS },
      })
        .then((res) => !stale && setInfo({ key, info: res }))
        .catch(
          (e) =>
            !stale &&
            setInfo({
              key,
              info: {
                statuspage: false,
                error: e instanceof Error ? e.message : String(e),
              },
            })
        )
        .finally(() => !stale && setLoading(false))
    }, 700)
    return () => {
      stale = true
      clearTimeout(timer)
    }
    // key covers target and ignoreTLS
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, active])

  return {
    info: active && info?.key === key ? info.info : null,
    loading: active && loading,
  }
}

/** Offers the statuspage type on an http/keyword monitor that points at one. */
export function StatuspageSuggest({
  info,
  onUse,
}: {
  info: StatuspageInfo | null
  onUse: () => void
}) {
  const t = useTranslations("statuspage")
  if (!info?.statuspage) return null
  return (
    <div className="flex flex-wrap items-center gap-3 border border-info/40 bg-info/5 p-3 text-xs sm:col-span-2">
      <p className="min-w-0 flex-1">
        {t("suggest", { name: info.name || info.url || "" })}
      </p>
      <Button type="button" size="sm" onClick={onUse}>
        {t("use")}
      </Button>
    </div>
  )
}

const states = [
  "operational",
  "degraded_performance",
  "partial_outage",
  "major_outage",
  "under_maintenance",
] as const
type State = (typeof states)[number]

const tone: Record<State, string> = {
  operational: "text-up",
  degraded_performance: "text-warn",
  partial_outage: "text-warn",
  major_outage: "text-down",
  under_maintenance: "text-info",
}

/** Page state and the components to follow, kept in the keyword field as ids. */
export function StatuspageComponents({
  info,
  loading,
  value,
  onChange,
}: {
  info: StatuspageInfo | null
  loading: boolean
  value: string
  onChange: (v: string) => void
}) {
  const t = useTranslations("statuspage")
  const ids = value
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean)
  const comps = info?.components ?? []
  const known = new Set(comps.map((c) => c.id))
  const unknown = ids.filter((id) => !known.has(id))

  function toggle(id: string, on: boolean) {
    const next = on ? [...new Set([...ids, id])] : ids.filter((x) => x !== id)
    onChange(next.join(", "))
  }

  // Components in page order, under their group's name.
  const groups: { name: string; items: StatuspageComponent[] }[] = []
  for (const c of comps) {
    const name = c.group ?? ""
    let g = groups.find((x) => x.name === name)
    if (!g) {
      g = { name, items: [] }
      groups.push(g)
    }
    g.items.push(c)
  }

  return (
    <div className="grid gap-2 sm:col-span-2">
      <p className="text-[11px] text-muted-foreground">{t("mapping")}</p>
      {loading && !info && (
        <p className="text-xs text-muted-foreground">{t("loading")}</p>
      )}
      {info && !info.statuspage && (
        <p className="text-xs text-down">! {info.error || t("notPage")}</p>
      )}
      {info?.statuspage && (
        <>
          <p className="text-xs">
            {info.name}:{" "}
            <span
              className={cn(
                info.indicator === "none"
                  ? "text-up"
                  : info.indicator === "minor"
                    ? "text-warn"
                    : info.indicator === "maintenance"
                      ? "text-info"
                      : "text-down"
              )}
            >
              {info.description}
            </span>
          </p>
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-medium">{t("components")}</span>
            {ids.length > 0 && (
              <button
                type="button"
                className="text-[11px] text-muted-foreground underline"
                onClick={() => onChange("")}
              >
                {t("wholePage")}
              </button>
            )}
          </div>
          <p className="text-[11px] text-muted-foreground">
            {ids.length === 0
              ? t("componentsNone")
              : t("componentsSome", { count: ids.length })}
          </p>
          <div className="grid max-h-72 gap-3 overflow-y-auto border p-3">
            {groups.map((g) => (
              <div key={g.name} className="grid gap-1.5">
                {g.name && (
                  <span className="text-[11px] text-muted-foreground uppercase">
                    {g.name}
                  </span>
                )}
                {g.items.map((c) => (
                  <label key={c.id} className="flex items-center gap-2 text-xs">
                    <Checkbox
                      checked={ids.includes(c.id)}
                      onCheckedChange={(v) => toggle(c.id, v)}
                    />
                    <span className="min-w-0 flex-1 truncate">{c.name}</span>
                    <span
                      className={cn(
                        "shrink-0",
                        tone[c.status as State] ?? "text-muted-foreground"
                      )}
                    >
                      {t(
                        `states.${(states as readonly string[]).includes(c.status) ? (c.status as State) : "other"}`
                      )}
                    </span>
                  </label>
                ))}
              </div>
            ))}
          </div>
          {unknown.length > 0 && (
            <p className="text-xs text-warn">
              {t("unknown", { ids: unknown.join(", ") })}{" "}
              <button
                type="button"
                className="underline"
                onClick={() =>
                  onChange(ids.filter((id) => known.has(id)).join(", "))
                }
              >
                {t("removeUnknown")}
              </button>
            </p>
          )}
        </>
      )}
    </div>
  )
}
