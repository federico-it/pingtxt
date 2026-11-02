"use client"

import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Empty, Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  api,
  auditSources,
  useApi,
  useIsAdmin,
  type AuditEntry,
} from "@/lib/api"
import { datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

const periods = ["24h", "7d", "30d", "90d", "all"] as const
type Period = (typeof periods)[number]
const periodMs: Record<Period, number> = {
  "24h": 864e5,
  "7d": 7 * 864e5,
  "30d": 30 * 864e5,
  "90d": 90 * 864e5,
  all: 0,
}

// Action prefixes offered in the filter. Names stay as the hub writes them
// (monitor.update...), like commands in a terminal.
const categories = [
  "monitor",
  "group",
  "tag",
  "maintenance",
  "notifier",
  "page",
  "page_incident",
  "agent",
  "user",
  "account",
  "token",
  "app",
  "settings",
  "backup",
  "config",
  "report",
  "audit",
  "mcp",
] as const

const ALL = "all"

/** Query string for /api/audit from the page's URL filters. */
function apiQuery(params: URLSearchParams): URLSearchParams {
  const q = new URLSearchParams()
  const period = (params.get("period") as Period) || "7d"
  if (periodMs[period]) q.set("since", String(Date.now() - periodMs[period]))
  for (const k of ["user", "source", "category", "target_type", "target_id", "q", "request_id"]) {
    const v = params.get(k)
    if (v) q.set(k, v)
  }
  if (params.get("problems") === "1") q.set("problems", "1")
  return q
}

/** The admin's audit log: filters in the URL, pages of 50, export. */
export function AuditLog() {
  const t = useTranslations("audit")
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const users = useApi<{ id: number; email: string }[]>("/api/audit/users", { refreshInterval: 0 })

  type Page = { entries: AuditEntry[]; next_before: number }
  const [more, setMore] = useState<{ key: string; entries: AuditEntry[]; next: number } | null>(null)
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState(params.get("q") ?? "")

  const query = useMemo(() => apiQuery(params), [params])

  const set = useCallback(
    (changes: Record<string, string | null>) => {
      const q = new URLSearchParams(params.toString())
      for (const [k, v] of Object.entries(changes)) {
        if (v === null || v === "" || v === ALL) q.delete(k)
        else q.set(k, v)
      }
      const s = q.toString()
      router.replace(pathname + (s ? "?" + s : ""), { scroll: false })
    },
    [params, pathname, router]
  )

  // Typing searches after a short pause.
  useEffect(() => {
    const id = setTimeout(() => {
      if ((params.get("q") ?? "") !== search) set({ q: search })
    }, 400)
    return () => clearTimeout(id)
  }, [search, params, set])

  const key = "/api/audit?" + query.toString()
  const first = useApi<Page>(key, { refreshInterval: 0 })
  const extra = more?.key === key ? more : null
  const entries = first.data ? [...first.data.entries, ...(extra?.entries ?? [])] : null
  const next = extra ? extra.next : (first.data?.next_before ?? 0)
  const error = first.error instanceof Error ? first.error.message : ""

  async function loadMore() {
    setLoading(true)
    try {
      const q = new URLSearchParams(query)
      q.set("before", String(next))
      const r = await api<Page>("/api/audit?" + q.toString())
      setMore({ key, entries: [...(extra?.entries ?? []), ...r.entries], next: r.next_before })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setLoading(false)
    }
  }

  const period = (params.get("period") as Period) || "7d"
  const source = params.get("source") || ALL
  const category = params.get("category") || ALL
  const user = params.get("user") || ALL
  const target = params.get("target_type")
  const requestID = params.get("request_id")

  const periodItems = periods.map((p) => ({ value: p, label: t(`periods.${p}`) }))
  const sourceItems = [{ value: ALL, label: t("allSources") }, ...auditSources.map((s) => ({ value: s, label: t(`sources.${s}`) }))]
  const categoryItems = [{ value: ALL, label: t("allActions") }, ...categories.map((c) => ({ value: c, label: c + ".*" }))]
  const userItems = [
    { value: ALL, label: t("allUsers") },
    { value: "-1", label: t("noUser") },
    ...(users.data ?? []).map((u) => ({ value: String(u.id), label: u.email })),
  ]

  const exportQuery = new URLSearchParams(query)

  return (
    <>
      <p className="text-xs text-muted-foreground">{t("intro")}</p>
      <Section
        title={t("filters")}
        actions={
          <span className="flex gap-1">
            <Button size="xs" variant="ghost" render={<a href={"/api/audit/export?" + exportQuery.toString()} download />} nativeButton={false}>
              {t("exportCsv")}
            </Button>
            <Button
              size="xs"
              variant="ghost"
              render={<a href={"/api/audit/export?" + new URLSearchParams([...exportQuery, ["format", "jsonl"]]).toString()} download />}
              nativeButton={false}
            >
              {t("exportJsonl")}
            </Button>
          </span>
        }
      >
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <FilterSelect label={t("period")} items={periodItems} value={period} onChange={(v) => set({ period: v === "7d" ? null : v })} />
          <FilterSelect label={t("user")} items={userItems} value={user} onChange={(v) => set({ user: v })} />
          <FilterSelect label={t("source")} items={sourceItems} value={source} onChange={(v) => set({ source: v })} />
          <FilterSelect label={t("action")} items={categoryItems} value={category} onChange={(v) => set({ category: v })} />
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <Input
            className="max-w-xs"
            placeholder={t("search")}
            aria-label={t("search")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <label className="flex items-center gap-2 text-xs">
            <Checkbox checked={params.get("problems") === "1"} onCheckedChange={(v) => set({ problems: v ? "1" : null })} />
            {t("problemsOnly")}
          </label>
          {target && (
            <Chip onClear={() => set({ target_type: null, target_id: null })}>
              {target} #{params.get("target_id")}
            </Chip>
          )}
          {requestID && <Chip onClear={() => set({ request_id: null })}>{t("sameRequest")}</Chip>}
        </div>
      </Section>

      <Section title={t("entries")}>
        {error && <p className="mb-2 text-xs text-down">{error}</p>}
        {entries === null ? null : entries.length === 0 ? (
          <Empty>{t("empty")}</Empty>
        ) : (
          <div className="divide-y divide-dashed border border-dashed">
            {entries.map((e) => (
              <AuditRow key={e.id} e={e} onRequest={(id) => set({ request_id: id })} />
            ))}
          </div>
        )}
        {next > 0 && (
          <div className="mt-3">
            <Button variant="outline" disabled={loading} onClick={loadMore}>
              {t("more")}
            </Button>
          </div>
        )}
      </Section>
    </>
  )
}

function FilterSelect({
  label,
  items,
  value,
  onChange,
}: {
  label: string
  items: { value: string; label: string }[]
  value: string
  onChange: (v: string) => void
}) {
  return (
    <Select items={items} value={value} onValueChange={(v) => v && onChange(v)}>
      <SelectTrigger className="w-full" aria-label={label}>
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
  )
}

function Chip({ children, onClear }: { children: React.ReactNode; onClear: () => void }) {
  const t = useTranslations("audit")
  return (
    <span className="inline-flex h-6 items-center gap-1 border border-dashed px-2 text-xs">
      {children}
      <button type="button" className="text-muted-foreground hover:text-foreground" aria-label={t("clear")} onClick={onClear}>
        ×
      </button>
    </span>
  )
}

/** The entry is about its own user (logins, password...): the name adds nothing. */
function isSelf(e: AuditEntry) {
  return e.target_type === "user" && e.target_id === e.user_id
}

function resultClass(r: AuditEntry["result"]) {
  return r === "ok" ? "text-muted-foreground" : "text-down"
}

/** Who did it, in one short label. */
export function actor(e: AuditEntry, t: ReturnType<typeof useTranslations>) {
  if (e.user_email) return e.user_email
  return e.source === "system" ? t("sources.system") : e.source === "cli" ? t("sources.cli") : "?"
}

function AuditRow({ e, onRequest }: { e: AuditEntry; onRequest: (id: string) => void }) {
  const t = useTranslations("audit")
  const [open, setOpen] = useState(false)
  return (
    <div className="text-xs">
      <button
        type="button"
        className="flex w-full flex-wrap gap-x-3 px-3 py-2 text-left hover:bg-accent/50 sm:grid sm:grid-cols-[9.5rem_minmax(0,11rem)_3.5rem_minmax(0,1fr)_3.5rem]"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <span className="text-muted-foreground tabular-nums">{datetime(e.at)}</span>
        <span className="max-w-full truncate">{actor(e, t)}</span>
        <span className="text-muted-foreground">[{e.source}]</span>
        <span className="w-full min-w-0 truncate sm:w-auto">
          <span className="text-primary">{e.action}</span> {e.target_name && !isSelf(e) && <span>{e.target_name}</span>}
        </span>
        <span className={cn("sm:text-right", resultClass(e.result))}>{t(`results.${e.result}`)}</span>
      </button>
      {open && <AuditDetail e={e} onRequest={onRequest} />}
    </div>
  )
}

function show(v: unknown): string {
  if (v === null || v === undefined || v === "") return "—"
  if (typeof v === "string") return v
  return JSON.stringify(v)
}

function AuditDetail({ e, onRequest }: { e: AuditEntry; onRequest: (id: string) => void }) {
  const t = useTranslations("audit")
  const fields = Object.entries(e.changes ?? {}).filter(([k]) => !k.startsWith("_"))
  const details = Object.entries(e.changes ?? {}).filter(([k]) => k.startsWith("_"))
  const meta: [string, string][] = [
    [t("ip"), e.ip],
    [t("connection"), e.remote_addr],
    [t("credential"), e.credential],
    [t("tool"), e.via],
    [t("userAgent"), e.user_agent],
    [t("target"), e.target_type ? `${e.target_type} #${e.target_id}` : ""],
  ]
  return (
    <div className="grid gap-3 border-t border-dashed bg-muted/30 px-3 py-3">
      {fields.length > 0 && (
        <div className="grid gap-1">
          {fields.map(([k, v]) => {
            const [before, after] = Array.isArray(v) ? v : [undefined, v]
            return (
              <div key={k} className="grid gap-x-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
                <span className="truncate text-muted-foreground">{k}</span>
                <span className="min-w-0 break-all">
                  <span className="text-down line-through decoration-down/50">{show(before)}</span>{" "}
                  <span className="text-muted-foreground">→</span> <span className="text-up">{show(after)}</span>
                </span>
              </div>
            )
          })}
        </div>
      )}
      {details.length > 0 && (
        <div className="grid gap-1">
          {details.map(([k, v]) => (
            <div key={k} className="grid gap-x-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
              <span className="truncate text-muted-foreground">{k.slice(1)}</span>
              <span className="min-w-0 break-all">{show(v)}</span>
            </div>
          ))}
        </div>
      )}
      <div className="grid gap-1 text-[11px]">
        {meta
          .filter(([, v]) => v)
          .map(([k, v]) => (
            <div key={k} className="grid gap-x-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
              <span className="text-muted-foreground">{k}</span>
              <span className="min-w-0 break-all">{v}</span>
            </div>
          ))}
        {e.ip && e.remote_addr && !e.remote_addr.startsWith(e.ip) && !e.remote_addr.startsWith("[" + e.ip) && (
          <p className="text-muted-foreground">{t("ipFromHeader")}</p>
        )}
      </div>
      <div>
        <Button size="xs" variant="ghost" onClick={() => onRequest(e.request_id)}>
          {t("showRequest")}
        </Button>
      </div>
    </div>
  )
}

/**
 * Last changes to one object (monitor, channel, status page), for admins,
 * with a link to the full log filtered on it.
 */
export function AuditTrail({ type, id }: { type: string; id: number }) {
  const t = useTranslations("audit")
  const isAdmin = useIsAdmin()
  const { data } = useApi<{ entries: AuditEntry[] }>(
    isAdmin && id ? `/api/audit?target_type=${type}&target_id=${id}&limit=10` : null,
    { refreshInterval: 60_000 }
  )
  if (!isAdmin || !data) return null
  return (
    <Section
      title={t("changes")}
      actions={
        <Link href={`/settings/audit/?target_type=${type}&target_id=${id}&period=all`} className="normal-case hover:text-foreground">
          {t("fullLog")} →
        </Link>
      }
    >
      {data.entries.length === 0 ? (
        <div className="text-xs text-muted-foreground">{t("noChanges")}</div>
      ) : (
        <div className="grid grid-cols-[minmax(0,1fr)] gap-1">
          {data.entries.map((e) => (
            <div key={e.id} className="flex min-w-0 flex-wrap gap-x-3 text-xs">
              <span className="text-muted-foreground tabular-nums">{datetime(e.at)}</span>
              <span className="text-primary">{e.action}</span>
              <span>{actor(e, t)}</span>
              <span className="text-muted-foreground">[{e.source}]</span>
              {e.result !== "ok" && <span className="text-down">{t(`results.${e.result}`)}</span>}
              <span className="min-w-0 flex-1 truncate text-muted-foreground">
                {changeSummary(e, t)}
              </span>
            </div>
          ))}
        </div>
      )}
    </Section>
  )
}

/** Short description of an entry's changes: a created or deleted object
 * says so, an update lists the fields that changed. */
function changeSummary(e: AuditEntry, t: ReturnType<typeof useTranslations>) {
  if (e.action.endsWith(".create")) return t("created")
  if (e.action.endsWith(".delete")) return t("deleted")
  return Object.keys(e.changes ?? {})
    .filter((k) => !k.startsWith("_"))
    .join(", ")
}

/** The user's own logins, two-factor changes, tokens and app sign-ins. */
export function AccountActivity() {
  const t = useTranslations("audit")
  const { data } = useApi<AuditEntry[]>("/api/account/activity", { refreshInterval: 60_000 })
  if (!data) return null
  return (
    <>
      <p className="mb-2 text-xs text-muted-foreground">{t("activityIntro")}</p>
      {data.length === 0 ? (
        <div className="text-xs text-muted-foreground">{t("noActivity")}</div>
      ) : (
        <div className="divide-y divide-dashed border border-dashed">
          {data.map((e) => {
            const method = e.changes?._method
            return (
              <div key={e.id} className="flex flex-wrap gap-x-3 px-3 py-2 text-xs">
                <span className="text-muted-foreground tabular-nums">{datetime(e.at)}</span>
                <span className="text-primary">{e.action}</span>
                {typeof method === "string" && <span>{method}</span>}
                {e.action.startsWith("app.") || e.action.startsWith("token.") ? <span>{e.target_name}</span> : null}
                <span className="text-muted-foreground">[{e.source}]</span>
                <span className="text-muted-foreground">{e.ip}</span>
                <span className={cn("ml-auto", resultClass(e.result))}>{t(`results.${e.result}`)}</span>
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}
