"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { useMemo, useState } from "react"
import { ArrowDownIcon, ArrowUpIcon, PlusIcon, XIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { GroupToggle, useLabels } from "@/components/labels"
import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { StatusGlyph } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { api, useApi, type Monitor, type Notifier, type StatusPage } from "@/lib/api"
import { useMounted } from "@/hooks/use-now"

type Draft = Omit<StatusPage, "id" | "created_at">

const empty: Draft = {
  slug: "",
  title: "",
  description: "",
  domain: "",
  published: true,
  show_incidents: true,
  monitor_ids: [],
  group_ids: [],
  subscribe_notifier_id: 0,
}

function slugify(s: string) {
  return s
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 50)
}

export function StatusPageForm({ initial }: { initial?: StatusPage }) {
  const router = useRouter()
  const mounted = useMounted()
  const monitors = useApi<Monitor[]>("/api/monitors", { refreshInterval: 0 })
  const notifiers = useApi<Notifier[]>("/api/notifiers", { refreshInterval: 0 })
  const [d, setD] = useState<Draft>(initial ? { ...initial, group_ids: initial.group_ids ?? [] } : empty)
  const { groups, groupById } = useLabels()
  const [slugTouched, setSlugTouched] = useState(!!initial)
  const [busy, setBusy] = useState(false)
  const t = useTranslations("statusPageForm")

  const byId = useMemo(() => new Map((monitors.data ?? []).map((m) => [m.id, m])), [monitors.data])
  // Monitors of a picked group are on the page already.
  const available = (monitors.data ?? []).filter((m) => !d.monitor_ids.includes(m.id) && !d.group_ids.includes(m.group_id))
  const origin = mounted ? window.location.origin : ""
  const emailChannels = (notifiers.data ?? []).filter((n) => n.type === "email")
  const channelItems = [
    { value: "0", label: t("subscribeOff") },
    ...emailChannels.map((n) => ({ value: String(n.id), label: n.enabled ? n.name : `${n.name} (${t("channelDisabled")})` })),
  ]

  function set<K extends keyof Draft>(k: K, v: Draft[K]) {
    setD((p) => ({ ...p, [k]: v }))
  }

  function move(k: "monitor_ids" | "group_ids", i: number, by: number) {
    setD((p) => {
      const ids = [...p[k]]
      const j = i + by
      if (j < 0 || j >= ids.length) return p
      ;[ids[i], ids[j]] = [ids[j], ids[i]]
      return { ...p, [k]: ids }
    })
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const saved = await api<StatusPage>(initial ? `/api/status-pages/${initial.id}` : "/api/status-pages", {
        method: initial ? "PUT" : "POST",
        body: d,
      })
      toast.success(t("saved"))
      if (!initial) router.replace(`/status-pages/edit/?id=${saved.id}`)
      else setD(saved)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="grid max-w-3xl gap-2">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("pageTitle")} htmlFor="sp-title">
          <Input
            id="sp-title"
            required
            maxLength={100}
            value={d.title}
            placeholder={t("titlePlaceholder")}
            onChange={(e) => {
              const title = e.target.value
              setD((p) => ({ ...p, title, slug: slugTouched ? p.slug : slugify(title) }))
            }}
          />
        </Field>
        <Field label={t("slug")} htmlFor="sp-slug" hint={origin && `${origin}/status/${d.slug || "..."}/`}>
          <Input
            id="sp-slug"
            required
            pattern="[a-z0-9](?:[a-z0-9\-]*[a-z0-9])?"
            maxLength={50}
            value={d.slug}
            placeholder="acme"
            onChange={(e) => {
              setSlugTouched(true)
              set("slug", e.target.value.toLowerCase())
            }}
          />
        </Field>
        <Field label={t("description")} htmlFor="sp-desc" className="sm:col-span-2">
          <Textarea
            id="sp-desc"
            maxLength={1000}
            value={d.description}
            placeholder={t("descriptionPlaceholder")}
            onChange={(e) => set("description", e.target.value)}
          />
        </Field>
        <Field
          label={t("domain")}
          htmlFor="sp-domain"
          className="sm:col-span-2"
          hint={
            t.rich("domainHint", { code: (c) => <code>{c}</code> })
          }
        >
          <Input id="sp-domain" value={d.domain} placeholder="status.example.com" onChange={(e) => set("domain", e.target.value)} />
        </Field>
        <label className="flex items-center gap-2.5">
          <Switch checked={d.published} onCheckedChange={(v) => set("published", v)} />
          <span>
            {t("published")}
            <span className="block text-[11px] text-muted-foreground">{t("publishedHint")}</span>
          </span>
        </label>
        <label className="flex items-center gap-2.5">
          <Switch checked={d.show_incidents} onCheckedChange={(v) => set("show_incidents", v)} />
          <span>
            {t("showIncidents")}
            <span className="block text-[11px] text-muted-foreground">{t("showIncidentsHint")}</span>
          </span>
        </label>
      </div>

      <Section title={t("subscribeTitle")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={t("subscribeChannel")}
            hint={emailChannels.length === 0 && notifiers.data ? t("subscribeNoChannel") : t("subscribeHint")}
          >
            <Select
              items={channelItems}
              value={String(d.subscribe_notifier_id ?? 0)}
              onValueChange={(v) => set("subscribe_notifier_id", Number(v ?? 0))}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {channelItems.map((c) => (
                  <SelectItem key={c.value} value={c.value}>
                    {c.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
      </Section>

      {(groups.data?.length ?? 0) > 0 && (
        <Section title={t("groupsTitle", { count: d.group_ids.length })}>
          <p className="mb-2 text-xs text-muted-foreground">
            {t("groupsHint")}
          </p>
          {d.group_ids.length > 0 && (
            <div className="mb-2 border-t border-dashed">
              {d.group_ids.map((id, i) => {
                const g = groupById.get(id)
                return (
                  <div key={id} className="flex items-center gap-2 border-b border-dashed px-2 py-1.5">
                    <span className="w-5 text-right text-xs text-muted-foreground">{i + 1}</span>
                    <span className="min-w-0 flex-1 truncate">
                      [{g?.name ?? `#${id}`}] <span className="text-xs text-muted-foreground">{t("monitorCount", { count: g?.monitors ?? 0 })}</span>
                    </span>
                    <Button type="button" variant="ghost" size="icon-xs" aria-label={t("moveUp")} disabled={i === 0} onClick={() => move("group_ids", i, -1)}>
                      <ArrowUpIcon />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      aria-label={t("moveDown")}
                      disabled={i === d.group_ids.length - 1}
                      onClick={() => move("group_ids", i, 1)}
                    >
                      <ArrowDownIcon />
                    </Button>
                    <Button type="button" variant="ghost" size="icon-xs" aria-label={t("remove")} onClick={() => set("group_ids", d.group_ids.filter((x) => x !== id))}>
                      <XIcon />
                    </Button>
                  </div>
                )
              })}
            </div>
          )}
          <div className="flex flex-wrap gap-1.5">
            {(groups.data ?? [])
              .filter((g) => !d.group_ids.includes(g.id))
              .map((g) => (
                <GroupToggle key={g.id} group={g} on={false} onChange={() => set("group_ids", [...d.group_ids, g.id])} />
              ))}
          </div>
        </Section>
      )}

      <Section title={t("monitorsTitle", { count: d.monitor_ids.length })}>
        {d.monitor_ids.length === 0 ? (
          <div className="border border-dashed p-4 text-center text-muted-foreground">
            {d.group_ids.length > 0 ? t("onlyGroups") : t("pickMonitors")}
          </div>
        ) : (
          <div className="border-t border-dashed">
            {d.monitor_ids.map((id, i) => {
              const m = byId.get(id)
              return (
                <div key={id} className="flex items-center gap-2 border-b border-dashed px-2 py-1.5">
                  <span className="w-5 text-right text-xs text-muted-foreground">{i + 1}</span>
                  {m && <StatusGlyph status={m.status} />}
                  <span className="min-w-0 flex-1 truncate">
                    {m?.name ?? `#${id}`}
                    {m && groupById.get(m.group_id) && <span className="ml-2 text-xs text-muted-foreground">[{groupById.get(m.group_id)!.name}]</span>}
                  </span>
                  <Button type="button" variant="ghost" size="icon-xs" aria-label={t("moveUp")} disabled={i === 0} onClick={() => move("monitor_ids", i, -1)}>
                    <ArrowUpIcon />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={t("moveDown")}
                    disabled={i === d.monitor_ids.length - 1}
                    onClick={() => move("monitor_ids", i, 1)}
                  >
                    <ArrowDownIcon />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={t("remove")}
                    onClick={() => set("monitor_ids", d.monitor_ids.filter((x) => x !== id))}
                  >
                    <XIcon />
                  </Button>
                </div>
              )
            })}
          </div>
        )}
      </Section>

      <Section
        title={t("availableTitle", { count: available.length })}
        actions={
          available.length > 0 && (
            <button type="button" className="hover:text-foreground" onClick={() => set("monitor_ids", [...d.monitor_ids, ...available.map((m) => m.id)])}>
              [{t("addAll")}]
            </button>
          )
        }
      >
        {monitors.data && available.length === 0 ? (
          <div className="text-xs text-muted-foreground">{t("allAdded")}</div>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {available.map((m) => (
              <Button key={m.id} type="button" variant="outline" size="sm" onClick={() => set("monitor_ids", [...d.monitor_ids, m.id])}>
                <PlusIcon /> {m.name}
              </Button>
            ))}
          </div>
        )}
      </Section>

      <div className="mt-8 flex flex-wrap gap-2">
        <Button type="submit" disabled={busy}>
          {busy ? t("saving") : t("save")}
        </Button>
        {initial && (
          <Button variant="outline" render={<a href={`/status/${initial.slug}/`} target="_blank" rel="noreferrer" />} nativeButton={false}>
            {t("openPage")}
          </Button>
        )}
        {initial && (
          <Button variant="outline" render={<Link href={`/status-pages/incidents/?id=${initial.id}`} />} nativeButton={false}>
            {t("incidents")}
          </Button>
        )}
        <Button type="button" variant="ghost" onClick={() => router.push("/status-pages/")}>
          {t("back")}
        </Button>
      </div>
    </form>
  )
}
