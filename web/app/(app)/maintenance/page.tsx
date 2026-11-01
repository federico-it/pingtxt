"use client"

import { useMemo, useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { GroupToggle, TagToggle, useLabels } from "@/components/labels"
import { Empty, PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useNow } from "@/hooks/use-now"
import { api, useApi, useIsAdmin, type Maintenance, type MaintenanceKind, type Monitor } from "@/lib/api"
import { datetime, duration } from "@/lib/format"
import { tr } from "@/lib/i18n"
import { chip } from "@/components/ui/control"
import { cn } from "@/lib/utils"

const kinds: MaintenanceKind[] = ["once", "daily", "weekly", "monthly"]
const weekdays = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"] as const
const dayName = (d: number) => tr(`maintenance.weekdays.${weekdays[d]}`)
// Monday first in the picker.
const weekdayOrder = [1, 2, 3, 4, 5, 6, 0]

type Draft = Omit<Maintenance, "id" | "created_at" | "active" | "active_end" | "next_start" | "next_end"> & { id?: number }

function browserTZ() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
  } catch {
    return "UTC"
  }
}

function timeZones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone")
  } catch {
    return ["UTC"]
  }
}

// <input type="datetime-local"> works in the browser's time zone.
function toLocalInput(ts: number) {
  if (!ts) return ""
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

function fromLocalInput(v: string) {
  if (!v) return 0
  const t = new Date(v).getTime()
  return Number.isNaN(t) ? 0 : t
}

function newDraft(): Draft {
  const start = Math.ceil(Date.now() / 300_000) * 300_000 // next 5 minutes
  return {
    name: "",
    description: "",
    all_monitors: false,
    monitor_ids: [],
    group_ids: [],
    tag_ids: [],
    kind: "once",
    start_at: start,
    end_at: start + 3600_000,
    start_time: "02:00",
    duration_min: 60,
    weekdays: [0],
    month_days: [1],
    timezone: browserTZ(),
    enabled: true,
  }
}

function minutes(n: number) {
  return duration(n * 60_000).replace(/ 0s$/, "")
}

function schedule(m: Maintenance) {
  if (m.kind === "once") return `${datetime(m.start_at).slice(0, 16)} → ${datetime(m.end_at).slice(0, 16)}`
  let days = tr("maintenance.everyDay")
  if (m.kind === "weekly")
    days = tr("maintenance.everyWeekday", { days: weekdayOrder.filter((d) => m.weekdays.includes(d)).map(dayName).join(", ") })
  if (m.kind === "monthly") days = tr("maintenance.onDays", { days: m.month_days.join(", ") })
  return tr("maintenance.scheduleLine", { days, time: m.start_time, tz: m.timezone, duration: minutes(m.duration_min) })
}

export default function MaintenancePage() {
  const isAdmin = useIsAdmin()
  const t = useTranslations("maintenance")
  const { data, mutate } = useApi<Maintenance[]>("/api/maintenances", { refreshInterval: 30_000 })
  const monitors = useApi<Monitor[]>("/api/monitors", { refreshInterval: 0 })
  const [draft, setDraft] = useState<Draft | null>(null)
  const now = useNow()
  const zones = useMemo(() => timeZones(), [])

  const names = useMemo(() => new Map((monitors.data ?? []).map((m) => [m.id, m.name])), [monitors.data])
  const { groups, tags, groupById, tagById } = useLabels()

  function set<K extends keyof Draft>(k: K, v: Draft[K]) {
    setDraft((p) => p && { ...p, [k]: v })
  }

  function toggle(k: "monitor_ids" | "group_ids" | "tag_ids" | "weekdays" | "month_days", v: number, on: boolean) {
    setDraft((p) => {
      if (!p) return p
      const list = p[k] as number[]
      return { ...p, [k]: on ? [...list, v] : list.filter((x) => x !== v) }
    })
  }

  // start_at/end_at are the window for "once" and an optional validity range
  // for recurring windows: reset them when switching between the two.
  function changeKind(kind: MaintenanceKind) {
    setDraft((p) => {
      if (!p) return p
      if ((p.kind === "once") === (kind === "once")) return { ...p, kind }
      const d = newDraft()
      return kind === "once" ? { ...p, kind, start_at: d.start_at, end_at: d.end_at } : { ...p, kind, start_at: 0, end_at: 0 }
    })
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    if (!draft) return
    try {
      await api(draft.id ? `/api/maintenances/${draft.id}` : "/api/maintenances", { method: draft.id ? "PUT" : "POST", body: draft })
      toast.success(t("saved"))
      setDraft(null)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function setEnabled(m: Maintenance, enabled: boolean) {
    try {
      await api(`/api/maintenances/${m.id}`, { method: "PUT", body: { ...m, enabled } })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
    mutate()
  }

  async function remove(m: Maintenance) {
    await api(`/api/maintenances/${m.id}`, { method: "DELETE" })
    mutate()
  }

  function scope(m: Maintenance) {
    if (m.all_monitors) return t("allMonitors")
    const list = [
      ...(m.group_ids ?? []).map((id) => groupById.get(id) && `[${groupById.get(id)!.name}]`),
      ...(m.tag_ids ?? []).map((id) => tagById.get(id) && `#${tagById.get(id)!.name}`),
      ...m.monitor_ids.map((id) => names.get(id)),
    ].filter(Boolean) as string[]
    if (list.length === 0) return t("noMonitors")
    return list.length <= 3 ? list.join(", ") : `${list.slice(0, 3).join(", ")} +${list.length - 3}`
  }

  function state(m: Maintenance) {
    if (!m.enabled) return <span className="text-muted-foreground">{t("disabled")}</span>
    if (m.active && m.active_end) return <span className="text-info">◇ {t("activeEnds", { in: duration(m.active_end - now) })}</span>
    if (m.next_start) return <span className="text-muted-foreground">{t("nextIn", { in: duration(m.next_start - now) })}</span>
    return <span className="text-muted-foreground">{t("ended")}</span>
  }

  const recurring = draft && draft.kind !== "once"

  return (
    <>
      <PageHeader cmd="pingtxt maint ls" title={t("title")}>
        {isAdmin && (
          <Button onClick={() => setDraft(newDraft())}>
            <PlusIcon /> {t("schedule")}
          </Button>
        )}
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        {t("intro")}
      </p>

      {data && data.length === 0 ? (
        <Empty>{t("empty")}</Empty>
      ) : (
        <div className="border-t border-dashed">
          {(data ?? []).map((m) => (
            <div key={m.id} className={cn("flex flex-wrap items-center gap-3 border-b border-dashed px-2 py-2.5", m.active && m.enabled && "bg-info/5")}>
              <Switch checked={m.enabled} onCheckedChange={(v) => setEnabled(m, v)} disabled={!isAdmin} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-baseline gap-x-3">
                  <span className="font-medium">{m.name}</span>
                  <span className="text-xs">{state(m)}</span>
                </div>
                <div className="truncate text-xs text-muted-foreground">
                  {schedule(m)} · {scope(m)}
                </div>
                {m.description && <div className="truncate text-xs text-muted-foreground">{m.description}</div>}
              </div>
              {isAdmin && (
                <>
                  <Button variant="ghost" size="xs" onClick={() => setDraft({ ...m, group_ids: m.group_ids ?? [], tag_ids: m.tag_ids ?? [] })}>
                    {t("edit")}
                  </Button>
                  <Confirm
                    title={t("deleteTitle", { name: m.name })}
                    onConfirm={() => remove(m)}
                    trigger={
                      <Button variant="ghost" size="xs" className="text-down">
                        {t("rm")}
                      </Button>
                    }
                  />
                </>
              )}
            </div>
          ))}
        </div>
      )}

      <Dialog open={!!draft} onOpenChange={(o) => !o && setDraft(null)}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-lg">
          {draft && (
            <form onSubmit={save} className="grid gap-4">
              <DialogHeader>
                <DialogTitle>{draft.id ? t("editTitle") : t("newTitle")}</DialogTitle>
                <DialogDescription>{t("dialogText")}</DialogDescription>
              </DialogHeader>

              <div className="grid gap-1.5">
                <Label htmlFor="mw-name">{t("name")}</Label>
                <Input id="mw-name" required value={draft.name} onChange={(e) => set("name", e.target.value)} placeholder={t("namePlaceholder")} />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="mw-desc">{t("notes")}</Label>
                <Textarea id="mw-desc" rows={2} value={draft.description} onChange={(e) => set("description", e.target.value)} />
              </div>

              <div className="grid gap-1.5">
                <Label>{t("repeat")}</Label>
                <Select items={kinds.map((k) => ({ value: k, label: t(`kinds.${k}`) }))} value={draft.kind} onValueChange={(v) => v && changeKind(v as MaintenanceKind)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {kinds.map((k) => (
                      <SelectItem key={k} value={k}>
                        {t(`kinds.${k}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {!recurring ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="grid gap-1.5">
                    <Label htmlFor="mw-start">{t("start")}</Label>
                    <Input id="mw-start" type="datetime-local" required value={toLocalInput(draft.start_at)} onChange={(e) => set("start_at", fromLocalInput(e.target.value))} />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="mw-end">{t("end")}</Label>
                    <Input id="mw-end" type="datetime-local" required value={toLocalInput(draft.end_at)} onChange={(e) => set("end_at", fromLocalInput(e.target.value))} />
                  </div>
                </div>
              ) : (
                <>
                  {draft.kind === "weekly" && (
                    <div className="grid gap-1.5">
                      <Label>{t("days")}</Label>
                      <div className="flex flex-wrap gap-1">
                        {weekdayOrder.map((d) => (
                          <DayToggle key={d} on={draft.weekdays.includes(d)} onChange={(on) => toggle("weekdays", d, on)}>
                            {dayName(d)}
                          </DayToggle>
                        ))}
                      </div>
                    </div>
                  )}
                  {draft.kind === "monthly" && (
                    <div className="grid gap-1.5">
                      <Label>{t("monthDays")}</Label>
                      <div className="grid grid-cols-7 gap-1">
                        {Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
                          <DayToggle key={d} on={draft.month_days.includes(d)} onChange={(on) => toggle("month_days", d, on)}>
                            {d}
                          </DayToggle>
                        ))}
                      </div>
                      <p className="text-xs text-muted-foreground">{t("monthDaysHint")}</p>
                    </div>
                  )}
                  <div className="grid gap-4 sm:grid-cols-3">
                    <div className="grid gap-1.5">
                      <Label htmlFor="mw-time">{t("at")}</Label>
                      <Input id="mw-time" type="time" required value={draft.start_time} onChange={(e) => set("start_time", e.target.value)} />
                    </div>
                    <div className="grid gap-1.5">
                      <Label htmlFor="mw-dur">{t("minutes")}</Label>
                      <Input id="mw-dur" type="number" min={1} max={10080} required value={draft.duration_min} onChange={(e) => set("duration_min", Number(e.target.value))} />
                    </div>
                    <div className="grid gap-1.5">
                      <Label htmlFor="mw-tz">{t("timezone")}</Label>
                      <Input id="mw-tz" list="mw-zones" required value={draft.timezone} onChange={(e) => set("timezone", e.target.value)} />
                      <datalist id="mw-zones">
                        {zones.map((z) => (
                          <option key={z} value={z} />
                        ))}
                      </datalist>
                    </div>
                  </div>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <div className="grid gap-1.5">
                      <Label htmlFor="mw-from">{t("from")}</Label>
                      <Input id="mw-from" type="datetime-local" value={toLocalInput(draft.start_at)} onChange={(e) => set("start_at", fromLocalInput(e.target.value))} />
                    </div>
                    <div className="grid gap-1.5">
                      <Label htmlFor="mw-until">{t("until")}</Label>
                      <Input id="mw-until" type="datetime-local" value={toLocalInput(draft.end_at)} onChange={(e) => set("end_at", fromLocalInput(e.target.value))} />
                    </div>
                  </div>
                </>
              )}

              <div className="grid gap-2">
                <div className="flex items-center justify-between">
                  <Label>{t("monitors")}</Label>
                  <label className="flex cursor-pointer items-center gap-2 text-xs">
                    <Switch checked={draft.all_monitors} onCheckedChange={(v) => set("all_monitors", v)} />
                    {t("allMonitors")}
                  </label>
                </div>
                {!draft.all_monitors && (groups.data?.length ?? 0) > 0 && (
                  <div className="flex flex-wrap items-center gap-1">
                    <span className="mr-1 text-xs text-muted-foreground">{t("groups")}</span>
                    {(groups.data ?? []).map((g) => (
                      <GroupToggle key={g.id} group={g} on={draft.group_ids.includes(g.id)} onChange={(on) => toggle("group_ids", g.id, on)} />
                    ))}
                  </div>
                )}
                {!draft.all_monitors && (tags.data?.length ?? 0) > 0 && (
                  <div className="flex flex-wrap items-center gap-1">
                    <span className="mr-1 text-xs text-muted-foreground">{t("tags")}</span>
                    {(tags.data ?? []).map((tg) => (
                      <TagToggle key={tg.id} tag={tg} on={draft.tag_ids.includes(tg.id)} onChange={(on) => toggle("tag_ids", tg.id, on)} />
                    ))}
                  </div>
                )}
                {!draft.all_monitors && (draft.group_ids.length > 0 || draft.tag_ids.length > 0) && (
                  <p className="text-xs text-muted-foreground">{t("labelsHint")}</p>
                )}
                {!draft.all_monitors && (
                  <div className="grid max-h-48 gap-1 overflow-y-auto sm:grid-cols-2">
                    {(monitors.data ?? []).map((m) => (
                      <label key={m.id} className="flex cursor-pointer items-center gap-2 border border-dashed px-2 py-1.5 hover:bg-accent/50">
                        <Checkbox checked={draft.monitor_ids.includes(m.id)} onCheckedChange={(v) => toggle("monitor_ids", m.id, v)} />
                        <span className="truncate">{m.name}</span>
                      </label>
                    ))}
                    {monitors.data?.length === 0 && <span className="text-xs text-muted-foreground">{t("noMonitorsYet")}</span>}
                  </div>
                )}
              </div>

              <label className="flex cursor-pointer items-center gap-2 text-sm">
                <Switch checked={draft.enabled} onCheckedChange={(v) => set("enabled", v)} />
                {t("enabled")}
              </label>

              <DialogFooter>
                <Button type="submit">{t("save")}</Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}

function DayToggle({ on, onChange, children }: { on: boolean; onChange: (on: boolean) => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={() => onChange(!on)}
      className={cn(
        chip,
        on ? "border-info bg-info/15 text-foreground" : "text-muted-foreground hover:text-foreground"
      )}
    >
      {children}
    </button>
  )
}
