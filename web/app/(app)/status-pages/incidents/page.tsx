"use client"

import Link from "next/link"
import { useSearchParams } from "next/navigation"
import { Suspense, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Empty, PageHeader, Section } from "@/components/page-header"
import { reportImpactBorder, reportImpactText, ReportStatusText, ReportTimeline } from "@/components/report-timeline"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import {
  api,
  reportImpacts,
  reportStatuses,
  useApi,
  useIsAdmin,
  type PageIncident,
  type PageIncidentUpdate,
  type PageSubscriber,
  type ReportImpact,
  type ReportStatus,
  type StatusPage,
} from "@/lib/api"
import { datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

type Saved = { incident: PageIncident; notified: number }

function Choice<T extends string>({
  value,
  items,
  onChange,
  id,
}: {
  value: T
  items: { value: T; label: string }[]
  onChange: (v: T) => void
  id?: string
}) {
  return (
    <Select items={items} value={value} onValueChange={(v) => v && onChange(v as T)}>
      <SelectTrigger className="w-full" id={id}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {items.map((x) => (
          <SelectItem key={x.value} value={x.value}>
            {x.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function useChoices() {
  const ts = useTranslations("reports.status")
  const ti = useTranslations("reports.impact")
  return {
    statusItems: reportStatuses.map((s) => ({ value: s, label: ts(s) })),
    impactItems: reportImpacts.map((s) => ({ value: s, label: ti(s) })),
  }
}

/** The status usually posted after the current one. */
function nextStatus(s: ReportStatus): ReportStatus {
  const i = reportStatuses.indexOf(s)
  return reportStatuses[Math.min(i + 1, reportStatuses.length - 1)]
}

function toastSaved(t: ReturnType<typeof useTranslations<"reports">>, r: Saved) {
  toast.success(r.notified > 0 ? t("postedNotified", { count: r.notified }) : t("posted"))
}

function NewIncident({ base, subscribers, onDone }: { base: string; subscribers: boolean; onDone: () => void }) {
  const t = useTranslations("reports")
  const { statusItems, impactItems } = useChoices()
  const [title, setTitle] = useState("")
  const [impact, setImpact] = useState<ReportImpact>("minor")
  const [status, setStatus] = useState<ReportStatus>("investigating")
  const [message, setMessage] = useState("")
  const [notify, setNotify] = useState(true)
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api<Saved>(`${base}/incidents`, { method: "POST", body: { title, impact, status, message, notify } })
      toastSaved(t, r)
      setTitle("")
      setMessage("")
      setStatus("investigating")
      onDone()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="grid gap-4 border border-dashed p-4 sm:grid-cols-4">
      <Field label={t("incidentTitle")} htmlFor="ri-title" className="sm:col-span-2">
        <Input id="ri-title" required maxLength={200} value={title} placeholder={t("titlePlaceholder")} onChange={(e) => setTitle(e.target.value)} />
      </Field>
      <Field label={t("impactLabel")} htmlFor="ri-impact">
        <Choice id="ri-impact" value={impact} items={impactItems} onChange={setImpact} />
      </Field>
      <Field label={t("statusLabel")} htmlFor="ri-status">
        <Choice id="ri-status" value={status} items={statusItems} onChange={setStatus} />
      </Field>
      <Field label={t("message")} htmlFor="ri-message" className="sm:col-span-4">
        <Textarea id="ri-message" required maxLength={5000} value={message} placeholder={t("messagePlaceholder")} onChange={(e) => setMessage(e.target.value)} />
      </Field>
      <div className="flex flex-wrap items-center gap-4 sm:col-span-4">
        <Button type="submit" disabled={busy}>
          {busy ? t("posting") : t("create")}
        </Button>
        {subscribers && <NotifyBox checked={notify} onChange={setNotify} />}
      </div>
    </form>
  )
}

function NotifyBox({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  const t = useTranslations("reports")
  return (
    <label className="flex cursor-pointer items-center gap-2 text-xs">
      <Checkbox checked={checked} onCheckedChange={(v) => onChange(!!v)} />
      {t("notify")}
    </label>
  )
}

function IncidentCard({
  base,
  incident,
  canWrite,
  subscribers,
  onChange,
}: {
  base: string
  incident: PageIncident
  canWrite: boolean
  subscribers: boolean
  onChange: () => void
}) {
  const t = useTranslations("reports")
  const ti = useTranslations("reports.impact")
  const { statusItems, impactItems } = useChoices()
  const url = `${base}/incidents/${incident.id}`
  const [status, setStatus] = useState<ReportStatus>(nextStatus(incident.status))
  const [message, setMessage] = useState("")
  const [notify, setNotify] = useState(true)
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState(false)
  // Resolved incidents rarely get updates: the form opens on request.
  const [reopen, setReopen] = useState(false)
  const [title, setTitle] = useState(incident.title)
  const [impact, setImpact] = useState<ReportImpact>(incident.impact)
  const open = !incident.resolved_at

  async function run(fn: () => Promise<void>) {
    setBusy(true)
    try {
      await fn()
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  const post = (e: React.FormEvent) => {
    e.preventDefault()
    run(async () => {
      const r = await api<Saved>(`${url}/updates`, { method: "POST", body: { status, message, notify } })
      toastSaved(t, r)
      setMessage("")
      setStatus(nextStatus(status))
    })
  }
  const saveEdit = (e: React.FormEvent) => {
    e.preventDefault()
    run(async () => {
      await api(url, { method: "PUT", body: { title, impact } })
      toast.success(t("saved"))
      setEditing(false)
    })
  }
  const deleteUpdate = (u: PageIncidentUpdate) =>
    run(async () => {
      await api(`${url}/updates/${u.id}`, { method: "DELETE" })
      toast.success(t("updateDeleted"))
    })
  const remove = () =>
    run(async () => {
      await api(url, { method: "DELETE" })
      toast.success(t("deleted"))
    })

  return (
    <div className={cn("border px-4 py-3", open ? reportImpactBorder[incident.impact] : "border-dashed")}>
      {editing ? (
        <form onSubmit={saveEdit} className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem_auto]">
          <Input required maxLength={200} value={title} aria-label={t("incidentTitle")} onChange={(e) => setTitle(e.target.value)} />
          <Choice value={impact} items={impactItems} onChange={setImpact} />
          <div className="flex gap-2">
            <Button type="submit" size="sm" disabled={busy}>
              {t("save")}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
              {t("cancel")}
            </Button>
          </div>
        </form>
      ) : (
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <ReportStatusText status={incident.status} className="text-xs uppercase" />
          <span className="min-w-0 flex-1 font-medium">{incident.title}</span>
          {incident.impact !== "none" && <span className={cn("text-xs", open ? reportImpactText[incident.impact] : "text-muted-foreground")}>[{ti(incident.impact)}]</span>}
          {canWrite && (
            <span className="flex gap-1">
              <Button type="button" variant="ghost" size="xs" onClick={() => setEditing(true)}>
                {t("edit")}
              </Button>
              <Confirm
                title={t("deleteTitle", { name: incident.title })}
                description={t("deleteText")}
                onConfirm={remove}
                trigger={
                  <Button type="button" variant="ghost" size="xs" className="text-down">
                    {t("rm")}
                  </Button>
                }
              />
            </span>
          )}
        </div>
      )}
      <div className="mt-1 text-xs text-muted-foreground">
        {t("started", { at: datetime(incident.created_at) })}
        {incident.resolved_at > 0 && ` · ${t("resolvedAt", { at: datetime(incident.resolved_at) })}`}
      </div>

      <div className="mt-3">
        <ReportTimeline incident={incident} onDelete={canWrite ? deleteUpdate : undefined} />
      </div>

      {canWrite && !open && !reopen && (
        <button type="button" className="mt-3 text-xs text-muted-foreground hover:text-foreground" onClick={() => setReopen(true)}>
          [{t("addUpdate")}]
        </button>
      )}
      {canWrite && (open || reopen) && (
        <form onSubmit={post} className="mt-4 grid gap-3 border-t border-dashed pt-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
          <Choice value={status} items={statusItems} onChange={setStatus} />
          <Textarea
            required
            maxLength={5000}
            rows={2}
            value={message}
            aria-label={t("message")}
            placeholder={t("updatePlaceholder")}
            onChange={(e) => setMessage(e.target.value)}
          />
          <div className="flex flex-wrap items-center gap-4 sm:col-start-2">
            <Button type="submit" size="sm" disabled={busy}>
              {t("postUpdate")}
            </Button>
            {subscribers && <NotifyBox checked={notify} onChange={setNotify} />}
          </div>
        </form>
      )}
    </div>
  )
}

function Subscribers({ base }: { base: string }) {
  const t = useTranslations("reports")
  const { data, mutate } = useApi<{ subscribers: PageSubscriber[]; enabled: boolean }>(`${base}/subscribers`, { refreshInterval: 0 })
  if (!data) return null
  const confirmed = data.subscribers.filter((s) => s.confirmed_at).length

  async function remove(s: PageSubscriber) {
    try {
      await api(`${base}/subscribers/${s.id}`, { method: "DELETE" })
      toast.success(t("subscriberRemoved"))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  return (
    <Section title={t("subscribersTitle", { confirmed, pending: data.subscribers.length - confirmed })}>
      {!data.enabled && <p className="mb-2 text-xs text-warn">{t("subscriptionsOff")}</p>}
      {data.subscribers.length === 0 ? (
        <div className="text-xs text-muted-foreground">{t("noSubscribers")}</div>
      ) : (
        <div className="border-t border-dashed">
          {data.subscribers.map((s) => (
            <div key={s.id} className="flex flex-wrap items-center gap-x-3 border-b border-dashed px-2 py-1.5">
              <span className="min-w-0 flex-1 truncate">{s.email}</span>
              <span className={cn("text-xs", s.confirmed_at ? "text-up" : "text-muted-foreground")}>
                {s.confirmed_at ? t("confirmed", { at: datetime(s.confirmed_at) }) : t("pending")}
              </span>
              <Confirm
                title={t("removeSubscriberTitle", { email: s.email })}
                onConfirm={() => remove(s)}
                trigger={
                  <Button type="button" variant="ghost" size="xs" className="text-down">
                    {t("rm")}
                  </Button>
                }
              />
            </div>
          ))}
        </div>
      )}
    </Section>
  )
}

function Incidents() {
  const t = useTranslations("reports")
  const tc = useTranslations("common")
  const id = Number(useSearchParams().get("id")) || 0
  const base = `/api/status-pages/${id}`
  const isAdmin = useIsAdmin()
  const page = useApi<StatusPage>(id ? base : null, { refreshInterval: 0 })
  const list = useApi<{ incidents: PageIncident[] }>(id ? `${base}/incidents` : null, { refreshInterval: 30_000 })

  if (!id) return <div className="text-down">! {t("noPage")}</div>
  if (page.error) return <div className="text-down">! {page.error.message}</div>
  if (!page.data) {
    return (
      <div className="text-muted-foreground">
        {tc("loading")}
        <span className="cursor-blink">_</span>
      </div>
    )
  }
  const p = page.data
  const subscribers = p.subscribe_notifier_id > 0
  const incidents = list.data?.incidents ?? []
  const open = incidents.filter((i) => !i.resolved_at)
  const past = incidents.filter((i) => i.resolved_at)

  return (
    <>
      <PageHeader cmd={`pingtxt status-pages incidents ${id}`} title={t("pageTitle", { name: p.title })}>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" render={<a href={`/status/${p.slug}/`} target="_blank" rel="noreferrer" />} nativeButton={false}>
            {t("openPage")}
          </Button>
          <Button variant="ghost" render={<Link href="/status-pages/" />} nativeButton={false}>
            {t("back")}
          </Button>
        </div>
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">{subscribers ? t("introSubscribers") : t("intro")}</p>

      {isAdmin && <NewIncident base={base} subscribers={subscribers} onDone={() => list.mutate()} />}

      <Section title={t("openTitle", { count: open.length })}>
        {open.length === 0 ? (
          <Empty>{t("noOpen")}</Empty>
        ) : (
          <div className="grid gap-3">
            {open.map((i) => (
              <IncidentCard key={`${i.id}-${i.updated_at}-${i.updates.length}`} base={base} incident={i} canWrite={!!isAdmin} subscribers={subscribers} onChange={() => list.mutate()} />
            ))}
          </div>
        )}
      </Section>

      {past.length > 0 && (
        <Section title={t("pastTitle", { count: past.length })}>
          <div className="grid gap-3">
            {past.map((i) => (
              <IncidentCard key={`${i.id}-${i.updated_at}-${i.updates.length}`} base={base} incident={i} canWrite={!!isAdmin} subscribers={subscribers} onChange={() => list.mutate()} />
            ))}
          </div>
        </Section>
      )}

      {isAdmin && <Subscribers base={base} />}
    </>
  )
}

export default function StatusPageIncidents() {
  return (
    <Suspense>
      <Incidents />
    </Suspense>
  )
}
