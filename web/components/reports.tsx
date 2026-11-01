"use client"

import { useMemo, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import {
  api,
  useApi,
  type Group,
  type Notifier,
  type Report,
  type ReportKind,
  type Reports,
  type ReportSettings,
} from "@/lib/api"
import { datetime } from "@/lib/format"
import { option } from "@/components/ui/control"

const kinds: ReportKind[] = ["weekly", "monthly"]

function timeZones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone")
  } catch {
    return ["UTC"]
  }
}

function browserTZ() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
  } catch {
    return "UTC"
  }
}

function settingsOf(r: Reports): ReportSettings {
  return {
    weekly: r.weekly,
    monthly: r.monthly,
    hour: r.hour,
    timezone: r.timezone,
    notifier_ids: r.notifier_ids,
    group_ids: r.group_ids,
  }
}

function Toggle({
  on,
  disabled,
  onClick,
  children,
}: {
  on: boolean
  disabled?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      aria-pressed={on}
      className={option({ active: on })}
    >
      {children}
    </button>
  )
}

/** Periodic email reports: settings, preview and "send now". */
export function PeriodicReports({ notifiers, isAdmin }: { notifiers: Notifier[]; isAdmin: boolean }) {
  const { data, mutate } = useApi<Reports>("/api/reports", { refreshInterval: 0 })
  const t = useTranslations("reports")
  return (
    <Section title={t("title")}>
      <p className="mb-4 max-w-2xl text-xs text-muted-foreground">{t("intro")}</p>
      {data && (
        <ReportsForm
          key={JSON.stringify(settingsOf(data))}
          reports={data}
          notifiers={notifiers}
          isAdmin={isAdmin}
          onSaved={(r) => mutate(r, { revalidate: false })}
        />
      )}
    </Section>
  )
}

function ReportsForm({
  reports,
  notifiers,
  isAdmin,
  onSaved,
}: {
  reports: Reports
  notifiers: Notifier[]
  isAdmin: boolean
  onSaved: (r: Reports) => void
}) {
  const t = useTranslations("reports")
  const groups = useApi<Group[]>("/api/groups", { refreshInterval: 0 })
  const [s, setS] = useState<ReportSettings>(() => {
    const v = settingsOf(reports)
    // Never saved: suggest the browser's time zone.
    if (!v.weekly && !v.monthly && v.timezone === "UTC" && v.notifier_ids.length === 0) v.timezone = browserTZ()
    return v
  })
  const [busy, setBusy] = useState(false)
  const [preview, setPreview] = useState<Report | null>(null)
  const zones = useMemo(() => timeZones(), [])
  const emails = notifiers.filter((n) => n.type === "email")
  const dirty = JSON.stringify(s) !== JSON.stringify(settingsOf(reports))

  function toggleID(key: "notifier_ids" | "group_ids", id: number) {
    setS((p) => ({ ...p, [key]: p[key].includes(id) ? p[key].filter((x) => x !== id) : [...p[key], id] }))
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api<Reports>("/api/reports", { method: "PUT", body: s })
      toast.success(t("saved"))
      onSaved(r)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  async function showPreview(kind: ReportKind) {
    try {
      setPreview(await api<Report>(`/api/reports/preview?kind=${kind}`))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function sendNow(kind: ReportKind) {
    const id = toast.loading(t("sending"))
    try {
      const r = await api<{ sent: number; error?: string }>("/api/reports/send", { method: "POST", body: { kind } })
      if (r.error) toast.warning(t("sentPartly", { count: r.sent, error: r.error }), { id })
      else toast.success(t("sent", { count: r.sent }), { id })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"), { id })
    }
  }

  const ro = !isAdmin
  return (
    <form onSubmit={save} className="grid max-w-2xl gap-5">
      <div className="grid gap-3">
        {kinds.map((k) => (
          <label key={k} className="flex flex-wrap items-center gap-2 text-xs">
            <Switch checked={s[k]} disabled={ro} onCheckedChange={(v) => setS((p) => ({ ...p, [k]: v }))} />
            <span className="text-foreground">{t(`kinds.${k}`)}</span>
            {reports.next[k] && !dirty && (
              <span className="text-muted-foreground">· {t("next", { date: datetime(reports.next[k]!).slice(0, 16) })}</span>
            )}
            {reports.last[k] && (
              <span className="text-muted-foreground">· {t("last", { date: datetime(reports.last[k]!.sent_at).slice(0, 16) })}</span>
            )}
          </label>
        ))}
      </div>

      <Field label={t("channels")} hint={emails.length ? t("channelsHint") : t("noEmail")}>
        {emails.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {emails.map((n) => (
              <Toggle key={n.id} on={s.notifier_ids.includes(n.id)} disabled={ro} onClick={() => toggleID("notifier_ids", n.id)}>
                {n.name}
                {!n.enabled && <span className="text-muted-foreground"> ({t("disabled")})</span>}
              </Toggle>
            ))}
          </div>
        )}
      </Field>

      <Field label={t("groups")} hint={t("groupsHint")}>
        <div className="flex flex-wrap gap-2">
          <Toggle on={s.group_ids.length === 0} disabled={ro} onClick={() => setS((p) => ({ ...p, group_ids: [] }))}>
            {t("allMonitors")}
          </Toggle>
          {(groups.data ?? []).map((g) => (
            <Toggle key={g.id} on={s.group_ids.includes(g.id)} disabled={ro} onClick={() => toggleID("group_ids", g.id)}>
              {g.name}
            </Toggle>
          ))}
        </div>
      </Field>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("hour")} htmlFor="rp-hour" hint={t("hourHint")}>
          <Input
            id="rp-hour"
            type="number"
            min={0}
            max={23}
            required
            disabled={ro}
            value={s.hour}
            onChange={(e) => setS((p) => ({ ...p, hour: Number(e.target.value) }))}
          />
        </Field>
        <Field label={t("timezone")} htmlFor="rp-tz">
          <Input
            id="rp-tz"
            list="rp-zones"
            required
            disabled={ro}
            value={s.timezone}
            onChange={(e) => setS((p) => ({ ...p, timezone: e.target.value }))}
          />
          <datalist id="rp-zones">
            {zones.map((z) => (
              <option key={z} value={z} />
            ))}
          </datalist>
        </Field>
      </div>

      <div className="flex flex-wrap gap-2">
        {isAdmin && (
          <Button type="submit" disabled={busy || !dirty}>
            {t("save")}
          </Button>
        )}
        {kinds.map((k) => (
          <Button key={k} type="button" variant="outline" onClick={() => showPreview(k)}>
            {t(`preview.${k}`)}
          </Button>
        ))}
      </div>

      {preview && (
        <div className="grid gap-2 border border-dashed p-3">
          <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
            <span className="min-w-0 break-words text-foreground">{preview.subject}</span>
            <span className="flex gap-1">
              {isAdmin && reports.notifier_ids.length > 0 && (
                <Button type="button" size="xs" variant="outline" onClick={() => sendNow(preview.kind)}>
                  {t("sendNow")}
                </Button>
              )}
              <Button type="button" size="xs" variant="ghost" onClick={() => setPreview(null)}>
                {t("close")}
              </Button>
            </span>
          </div>
          {dirty && <p className="text-[11px] text-muted-foreground">{t("previewHint")}</p>}
          <pre className="max-h-[28rem] overflow-auto text-[11px] leading-relaxed text-muted-foreground">{preview.text}</pre>
        </div>
      )}
    </form>
  )
}
