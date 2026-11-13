"use client"

import { useMemo, useState } from "react"
import Link from "next/link"
import { ArrowDownIcon, ArrowUpIcon, PhoneOffIcon, PlusIcon, XIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Empty, PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { api, useApi, useIsAdmin, type OnCallShift, type Schedule, type Me, type ScheduleView, type User } from "@/lib/api"
import { datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

const units = ["hours", "days", "weeks"] as const
const maxLen: Record<Schedule["shift_unit"], number> = { hours: 744, days: 92, weeks: 12 }

type Draft = Pick<Schedule, "name" | "description" | "timezone" | "members" | "shift_len" | "shift_unit" | "start_at"> & { id?: number }
type OverrideDraft = { schedule: ScheduleView; user_id: number; start_at: number; end_at: number }

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

/** Next Monday at 9:00 in the browser's time zone. */
function nextMonday() {
  const d = new Date()
  d.setHours(9, 0, 0, 0)
  d.setDate(d.getDate() + ((8 - d.getDay()) % 7 || 7))
  return d.getTime()
}

const newDraft = (): Draft => ({
  name: "",
  description: "",
  timezone: browserTZ(),
  members: [],
  shift_len: 1,
  shift_unit: "weeks",
  start_at: nextMonday(),
})

/** "2026-11-16 09:00" without seconds. */
const when = (ts: number) => datetime(ts).slice(0, 16)

export default function OnCallPage() {
  const t = useTranslations("oncall")
  const isAdmin = useIsAdmin()
  const { data, mutate } = useApi<ScheduleView[]>("/api/oncall", { refreshInterval: 60_000 })
  // Users can be listed by admins only: they are the ones who edit schedules.
  const users = useApi<User[]>(isAdmin ? "/api/users" : null, { refreshInterval: 0 })
  const [draft, setDraft] = useState<Draft | null>(null)
  const [override, setOverride] = useState<OverrideDraft | null>(null)
  const zones = useMemo(() => timeZones(), [])

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft((p) => p && { ...p, [k]: v })
  const email = (id: number) => users.data?.find((u) => u.id === id)?.email ?? `#${id}`

  async function save(e: React.FormEvent) {
    e.preventDefault()
    if (!draft) return
    try {
      await api(draft.id ? `/api/oncall/${draft.id}` : "/api/oncall", { method: draft.id ? "PUT" : "POST", body: draft })
      toast.success(t("saved"))
      setDraft(null)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function saveOverride(e: React.FormEvent) {
    e.preventDefault()
    if (!override) return
    try {
      const { schedule, ...body } = override
      await api(`/api/oncall/${schedule.id}/overrides`, { method: "POST", body })
      toast.success(t("overrideSaved"))
      setOverride(null)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function remove(s: ScheduleView) {
    try {
      await api(`/api/oncall/${s.id}`, { method: "DELETE" })
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function removeOverride(s: ScheduleView, id: number) {
    try {
      await api(`/api/oncall/${s.id}/overrides/${id}`, { method: "DELETE" })
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  function move(i: number, d: -1 | 1) {
    setDraft((p) => {
      if (!p) return p
      const m = [...p.members]
      ;[m[i], m[i + d]] = [m[i + d], m[i]]
      return { ...p, members: m }
    })
  }

  return (
    <>
      <PageHeader cmd="pingtxt oncall ls" title={t("title")}>
        {isAdmin && (
          <Button onClick={() => setDraft(newDraft())}>
            <PlusIcon /> {t("add")}
          </Button>
        )}
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        {t.rich("intro", {
          channels: (c) => (
            <Link href="/notifications/" className="underline underline-offset-2">
              {c}
            </Link>
          ),
        })}
      </p>

      {data && data.length === 0 ? (
        <Empty>{t("empty")}</Empty>
      ) : (
        <div className="grid gap-4">
          {(data ?? []).map((s) => (
            <ScheduleCard
              key={s.id}
              s={s}
              isAdmin={!!isAdmin}
              onEdit={() => setDraft({ id: s.id, name: s.name, description: s.description, timezone: s.timezone, members: s.members, shift_len: s.shift_len, shift_unit: s.shift_unit, start_at: s.start_at })}
              onOverride={() => {
                const start = Math.ceil(Date.now() / 900_000) * 900_000
                setOverride({ schedule: s, user_id: s.members[0] ?? 0, start_at: start, end_at: start + 24 * 3_600_000 })
              }}
              onDelete={() => remove(s)}
              onDeleteOverride={(id) => removeOverride(s, id)}
            />
          ))}
        </div>
      )}

      <Dialog open={!!draft} onOpenChange={(o) => !o && setDraft(null)}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
          {draft && (
            <form onSubmit={save} className="grid gap-4">
              <DialogHeader>
                <DialogTitle>{draft.id ? t("editTitle") : t("add")}</DialogTitle>
                <DialogDescription>{t("dialogText")}</DialogDescription>
              </DialogHeader>
              <div className="grid gap-1.5">
                <Label htmlFor="oc-name">{t("name")}</Label>
                <Input id="oc-name" required maxLength={100} value={draft.name} onChange={(e) => set("name", e.target.value)} placeholder={t("namePlaceholder")} />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="oc-desc">{t("description")}</Label>
                <Textarea id="oc-desc" rows={2} maxLength={1000} value={draft.description} onChange={(e) => set("description", e.target.value)} />
              </div>
              <div className="grid gap-1.5">
                <Label>{t("members")}</Label>
                <div className="grid border border-dashed text-sm">
                  {draft.members.map((id, i) => (
                    <div key={`${id}-${i}`} className="flex items-center gap-1 border-b border-dashed px-2 py-1 last:border-0">
                      <span className="w-5 text-xs text-muted-foreground">{i + 1}</span>
                      <span className="min-w-0 flex-1 truncate">{email(id)}</span>
                      {!users.data?.find((u) => u.id === id)?.phone && <NoPhone />}
                      <Button type="button" variant="ghost" size="icon-xs" disabled={i === 0} onClick={() => move(i, -1)} aria-label={t("up")}>
                        <ArrowUpIcon />
                      </Button>
                      <Button type="button" variant="ghost" size="icon-xs" disabled={i === draft.members.length - 1} onClick={() => move(i, 1)} aria-label={t("down")}>
                        <ArrowDownIcon />
                      </Button>
                      <Button type="button" variant="ghost" size="icon-xs" onClick={() => set("members", draft.members.filter((_, j) => j !== i))} aria-label={t("removeMember")}>
                        <XIcon />
                      </Button>
                    </div>
                  ))}
                  {draft.members.length === 0 && <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("noMembers")}</p>}
                </div>
                <Select
                  items={(users.data ?? []).map((u) => ({ value: String(u.id), label: u.email }))}
                  value={null}
                  onValueChange={(v) => v && set("members", [...draft.members, Number(v)])}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder={t("addMember")} />
                  </SelectTrigger>
                  <SelectContent>
                    {(users.data ?? []).map((u) => (
                      <SelectItem key={u.id} value={String(u.id)}>
                        {u.email}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">{t("membersHint")}</p>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="grid gap-1.5">
                  <Label htmlFor="oc-len">{t("shift")}</Label>
                  <Input
                    id="oc-len"
                    type="number"
                    min={1}
                    max={maxLen[draft.shift_unit]}
                    required
                    value={draft.shift_len}
                    onChange={(e) => set("shift_len", Math.max(1, Math.trunc(Number(e.target.value)) || 1))}
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label>{t("unit")}</Label>
                  <Select items={units.map((u) => ({ value: u, label: t(`units.${u}`) }))} value={draft.shift_unit} onValueChange={(v) => v && set("shift_unit", v as Schedule["shift_unit"])}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {units.map((u) => (
                        <SelectItem key={u} value={u}>
                          {t(`units.${u}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="col-span-2 grid gap-1.5 sm:col-span-1">
                  <Label htmlFor="oc-start">{t("start")}</Label>
                  <Input id="oc-start" type="datetime-local" required value={toLocalInput(draft.start_at)} onChange={(e) => set("start_at", fromLocalInput(e.target.value))} />
                </div>
                <div className="col-span-2 grid gap-1.5 sm:col-span-1">
                  <Label htmlFor="oc-tz">{t("timezone")}</Label>
                  <Input id="oc-tz" list="oc-zones" required value={draft.timezone} onChange={(e) => set("timezone", e.target.value)} />
                  <datalist id="oc-zones">
                    {zones.map((z) => (
                      <option key={z} value={z} />
                    ))}
                  </datalist>
                </div>
                <p className="col-span-2 text-xs text-muted-foreground">{t("startHint")}</p>
              </div>
              <DialogFooter>
                <Button type="submit" disabled={draft.members.length === 0}>
                  {t("save")}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={!!override} onOpenChange={(o) => !o && setOverride(null)}>
        <DialogContent className="sm:max-w-md">
          {override && (
            <form onSubmit={saveOverride} className="grid gap-4">
              <DialogHeader>
                <DialogTitle>{t("overrideTitle", { name: override.schedule.name })}</DialogTitle>
                <DialogDescription>{t("overrideText")}</DialogDescription>
              </DialogHeader>
              <div className="grid gap-1.5">
                <Label>{t("who")}</Label>
                <Select
                  items={(users.data ?? []).map((u) => ({ value: String(u.id), label: u.email }))}
                  value={override.user_id ? String(override.user_id) : null}
                  onValueChange={(v) => v && setOverride((p) => p && { ...p, user_id: Number(v) })}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(users.data ?? []).map((u) => (
                      <SelectItem key={u.id} value={String(u.id)}>
                        {u.email}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="col-span-2 grid gap-1.5 sm:col-span-1">
                  <Label htmlFor="ov-start">{t("from")}</Label>
                  <Input id="ov-start" type="datetime-local" required value={toLocalInput(override.start_at)} onChange={(e) => setOverride((p) => p && { ...p, start_at: fromLocalInput(e.target.value) })} />
                </div>
                <div className="col-span-2 grid gap-1.5 sm:col-span-1">
                  <Label htmlFor="ov-end">{t("until")}</Label>
                  <Input id="ov-end" type="datetime-local" required value={toLocalInput(override.end_at)} onChange={(e) => setOverride((p) => p && { ...p, end_at: fromLocalInput(e.target.value) })} />
                </div>
              </div>
              <DialogFooter>
                <Button type="submit" disabled={!override.user_id}>
                  {t("save")}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}

/** Missing phone number, shown only once a Twilio channel can text or call. */
function NoPhone() {
  const t = useTranslations("oncall")
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 60_000 })
  if (!me.data?.phone_alerts) return null
  return (
    <span title={t("noPhone")} className="text-warn">
      <PhoneOffIcon className="size-3" />
    </span>
  )
}

function ScheduleCard({
  s,
  isAdmin,
  onEdit,
  onOverride,
  onDelete,
  onDeleteOverride,
}: {
  s: ScheduleView
  isAdmin: boolean
  onEdit: () => void
  onOverride: () => void
  onDelete: () => void
  onDeleteOverride: (id: number) => void
}) {
  const t = useTranslations("oncall")
  const shifts = useApi<OnCallShift[]>(`/api/oncall/${s.id}/shifts`, { refreshInterval: 60_000 })
  const user = (id: number) => s.users.find((u) => u.id === id)
  const name = (id: number, fallback = "") => user(id)?.email ?? (fallback || t("nobody"))

  return (
    <section className="border border-dashed p-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <h2 className="font-medium">{s.name}</h2>
          <div className="text-xs text-muted-foreground">
            {[t("every", { count: s.shift_len, unit: s.shift_unit }), s.timezone].join(" · ")}
          </div>
          {s.description && <p className="mt-1 text-xs text-muted-foreground">{s.description}</p>}
        </div>
        {isAdmin && (
          <div className="flex flex-wrap gap-1">
            <Button variant="ghost" size="xs" onClick={onOverride}>
              {t("addOverride")}
            </Button>
            <Button variant="ghost" size="xs" onClick={onEdit}>
              {t("edit")}
            </Button>
            <Confirm
              title={t("deleteTitle", { name: s.name })}
              onConfirm={onDelete}
              trigger={
                <Button variant="ghost" size="xs" className="text-down">
                  {t("rm")}
                </Button>
              }
            />
          </div>
        )}
      </div>

      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <div className="border-l-2 border-up/60 pl-3">
          <div className="text-xs text-muted-foreground uppercase">{t("now")}</div>
          {s.current && s.current.user_id ? (
            <>
              <div className="flex items-center gap-1.5 truncate">
                {s.current.email}
                {!user(s.current.user_id)?.has_phone && <NoPhone />}
                {s.current.override && <span className="text-xs text-info">{t("override")}</span>}
              </div>
              <div className="text-xs text-muted-foreground">{t("untilTime", { time: when(s.current.end) })}</div>
            </>
          ) : (
            <div className="text-warn">{t("nobody")}</div>
          )}
        </div>
        <div className="border-l-2 pl-3">
          <div className="text-xs text-muted-foreground uppercase">{t("next")}</div>
          {s.next ? (
            <>
              <div className="truncate">{s.next.user_id ? s.next.email : t("nobody")}</div>
              <div className="text-xs text-muted-foreground">{t("fromTime", { time: when(s.next.start) })}</div>
            </>
          ) : (
            <div className="text-muted-foreground">—</div>
          )}
        </div>
      </div>

      <div className="mt-4 grid gap-4 text-xs md:grid-cols-2">
        <div>
          <div className="mb-1 text-muted-foreground uppercase">{t("rotation")}</div>
          <ol className="grid gap-0.5">
            {s.members.map((id, i) => (
              <li key={`${id}-${i}`} className="flex items-center gap-1.5">
                <span className="w-4 text-muted-foreground">{i + 1}</span>
                <span className="truncate">{name(id, `#${id}`)}</span>
                {user(id) && !user(id)?.has_phone && <NoPhone />}
              </li>
            ))}
          </ol>
        </div>
        <div>
          <div className="mb-1 text-muted-foreground uppercase">{t("upcoming")}</div>
          <ul className="grid gap-0.5">
            {(shifts.data ?? []).slice(0, 8).map((sh) => (
              <li key={sh.start} className={cn("flex gap-2", sh.override && "text-info")}>
                <span className="shrink-0 text-muted-foreground tabular-nums">{when(sh.start)}</span>
                <span className="truncate">{sh.user_id ? sh.email : t("nobody")}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>

      {s.overrides.length > 0 && (
        <div className="mt-4 text-xs">
          <div className="mb-1 text-muted-foreground uppercase">{t("overrides")}</div>
          <ul className="grid gap-0.5">
            {s.overrides.map((o) => (
              <li key={o.id} className="flex flex-wrap items-center gap-2">
                <span className="text-muted-foreground tabular-nums">
                  {when(o.start_at)} → {when(o.end_at)}
                </span>
                <span className="truncate">{name(o.user_id)}</span>
                {isAdmin && (
                  <button onClick={() => onDeleteOverride(o.id)} className="text-down hover:underline">
                    {t("rm")}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      <p className="mt-4 text-xs text-muted-foreground">
        {s.channels.length > 0
          ? t("alertedBy", { channels: s.channels.join(", ") })
          : t.rich("noChannel", {
              link: (c) => (
                <Link href="/notifications/" className="underline underline-offset-2">
                  {c}
                </Link>
              ),
            })}
      </p>
    </section>
  )
}
