"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { useMemo, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { AdminOnly } from "@/components/admin-only"
import { useLabels } from "@/components/labels"
import { Empty, PageHeader, Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { segment } from "@/components/ui/control"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { api, useApi, type Monitor, type Group } from "@/lib/api"
import { cn } from "@/lib/utils"

interface ServicePreset {
  id: string
  name: string
  category: string
  url: string
  color: string
  keywords: string[]
  monitor_id: number
}

interface PresetCatalog {
  categories: { id: string; name: string }[]
  services: ServicePreset[]
}

interface PresetResult {
  created: Monitor[]
  skipped: { id: string; monitor_id: number; reason: string }[]
  group?: Group
}

const intervals = [60, 120, 300] as const
const NEW_GROUP = "new"

export default function PresetsPage() {
  return (
    <AdminOnly>
      <Presets />
    </AdminOnly>
  )
}

function Presets() {
  const t = useTranslations("presets")
  const router = useRouter()
  const { data, error, mutate } = useApi<PresetCatalog>("/api/monitors/presets", {
    refreshInterval: 0,
    revalidateOnFocus: false,
  })
  const labels = useLabels()
  const [q, setQ] = useState("")
  const [cat, setCat] = useState("all")
  const [picked, setPicked] = useState<string[]>([])
  const [group, setGroup] = useState<string | null>(null)
  const [groupName, setGroupName] = useState(() => t("defaultGroup"))
  const [every, setEvery] = useState<number>(120)
  const [busy, setBusy] = useState(false)

  const services = useMemo(() => data?.services ?? [], [data])
  const query = q.trim().toLowerCase()
  const matches = (s: ServicePreset) =>
    !query ||
    s.name.toLowerCase().includes(query) ||
    s.id.includes(query) ||
    s.url.includes(query) ||
    s.keywords.some((k) => k.includes(query))
  const shown = services.filter((s) => (cat === "all" || s.category === cat) && matches(s))
  const counts = services.reduce<Record<string, number>>((acc, s) => {
    if (matches(s)) acc[s.category] = (acc[s.category] ?? 0) + 1
    return acc
  }, {})
  const total = Object.values(counts).reduce((a, b) => a + b, 0)

  // A group called like the default one is picked when it exists already.
  const existing = labels.tree.ordered.find((g) => g.name.toLowerCase() === t("defaultGroup").toLowerCase())
  const groupValue = group ?? (existing ? String(existing.id) : NEW_GROUP)
  const groupItems = [
    { value: "0", label: t("noGroup") },
    { value: NEW_GROUP, label: t("newGroup") },
    ...labels.tree.ordered.map((g) => ({ value: String(g.id), label: g.path })),
  ]

  function toggle(id: string) {
    setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))
  }

  async function add() {
    setBusy(true)
    try {
      const body: Record<string, unknown> = { ids: picked, interval_sec: every }
      if (groupValue === NEW_GROUP) body.group_name = groupName
      else body.group_id = Number(groupValue)
      const r = await api<PresetResult>("/api/monitors/presets", { method: "POST", body })
      toast.success(t("added", { count: r.created.length }))
      setPicked([])
      await mutate()
      router.push("/")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  // Categories the UI has no text for (a newer hub) show the hub's name.
  const catName = (c: { id: string; name: string }) =>
    t.has(`cats.${c.id}` as "cats.ai") ? t(`cats.${c.id}` as "cats.ai") : c.name
  const byCat = (data?.categories ?? [])
    .map((c) => ({ ...c, list: shown.filter((s) => s.category === c.id) }))
    .filter((c) => c.list.length > 0)
  const names = picked.map((id) => services.find((s) => s.id === id)?.name ?? id)

  return (
    <>
      <PageHeader cmd="pingtxt monitors add --service" title={t("title")}>
        <Input
          type="search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t("search")}
          className="w-48"
          autoFocus
        />
        <Button variant="outline" render={<Link href="/monitors/new/" />} nativeButton={false}>
          {t("custom")}
        </Button>
      </PageHeader>

      <p className="-mt-3 mb-4 max-w-3xl text-xs text-muted-foreground">{t("intro")}</p>

      <div className="mb-2 flex flex-wrap gap-1 text-xs">
        <button onClick={() => setCat("all")} className={segment({ active: cat === "all" })}>
          {t("all")}:{total}
        </button>
        {(data?.categories ?? []).map((c) => (
          <button key={c.id} onClick={() => setCat(c.id)} className={segment({ active: cat === c.id })}>
            {catName(c)}:{counts[c.id] ?? 0}
          </button>
        ))}
      </div>

      {error ? (
        <div className="text-down">! {error.message}</div>
      ) : !data ? (
        <div className="text-muted-foreground">
          {t("loading")}
          <span className="cursor-blink">_</span>
        </div>
      ) : shown.length === 0 ? (
        <Empty>{t("noMatch")}</Empty>
      ) : (
        byCat.map((c) => (
          <Section key={c.id} title={catName(c)} className="mt-6">
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {c.list.map((s) => (
                <ServiceCard key={s.id} s={s} on={picked.includes(s.id)} onToggle={() => toggle(s.id)} />
              ))}
            </div>
          </Section>
        ))
      )}

      {picked.length > 0 && (
        <div className="sticky bottom-4 z-20 mt-6 flex flex-wrap items-center gap-2 border border-dashed bg-background/95 px-3 py-2 text-xs shadow-lg backdrop-blur">
          <span className="mr-auto min-w-0 truncate">
            <span className="text-primary">$</span> {t("selected", { count: picked.length })}
            <span className="text-muted-foreground"> · {names.join(", ")}</span>
          </span>
          <span className="text-muted-foreground">{t("every")}</span>
          <div className="flex">
            {intervals.map((s) => (
              <button key={s} onClick={() => setEvery(s)} className={segment({ active: every === s })}>
                {t("minutes", { count: s / 60 })}
              </button>
            ))}
          </div>
          <span className="text-muted-foreground">{t("group")}</span>
          <Select items={groupItems} value={groupValue} onValueChange={(v) => setGroup(v ?? "0")}>
            <SelectTrigger size="sm" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {groupItems.map((g) => (
                <SelectItem key={g.value} value={g.value}>
                  {g.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {groupValue === NEW_GROUP && (
            <Input
              value={groupName}
              onChange={(e) => setGroupName(e.target.value)}
              aria-label={t("groupName")}
              className="h-7 w-32"
            />
          )}
          <Button size="sm" variant="ghost" onClick={() => setPicked([])} disabled={busy}>
            {t("clear")}
          </Button>
          <Button
            size="sm"
            onClick={add}
            disabled={busy || (groupValue === NEW_GROUP && !groupName.trim())}
          >
            {t("add", { count: picked.length })}
          </Button>
        </div>
      )}
    </>
  )
}

/** Readable text on a brand color: black on light colors, white on dark. */
function inkOn(hex: string) {
  const n = parseInt(hex.slice(1), 16)
  const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255]
  return 0.299 * r + 0.587 * g + 0.114 * b > 160 ? "#000" : "#fff"
}

function ServiceCard({ s, on, onToggle }: { s: ServicePreset; on: boolean; onToggle: () => void }) {
  const t = useTranslations("presets")
  const host = s.url.replace(/^https?:\/\//, "")
  const added = s.monitor_id > 0
  const inner = (
    <>
      <span
        aria-hidden
        className="flex size-9 shrink-0 items-center justify-center text-sm font-semibold ring-1 ring-foreground/10"
        style={{ background: s.color, color: inkOn(s.color) }}
      >
        {s.name.replace(/[^\p{L}\p{N}]/gu, "").charAt(0).toUpperCase()}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] text-foreground">{s.name}</span>
        <span className="block truncate text-[11px] text-muted-foreground">{host}</span>
      </span>
      <span className={cn("shrink-0 text-xs", on ? "text-primary" : "text-muted-foreground", added && "text-up")}>
        {added ? t("following") : on ? "[x]" : "[ ]"}
      </span>
    </>
  )
  const box = "flex min-h-14 items-center gap-3 border px-3 py-2 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
  if (added)
    return (
      <Link
        href={`/monitors/view/?id=${s.monitor_id}`}
        className={cn(box, "border-dashed opacity-70 hover:opacity-100")}
        title={t("followingHint", { name: s.name })}
      >
        {inner}
      </Link>
    )
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={on}
      title={s.keywords.length ? s.keywords.join(", ") : undefined}
      className={cn(box, on ? "border-primary bg-primary/5" : "border-dashed hover:bg-accent")}
    >
      {inner}
    </button>
  )
}
