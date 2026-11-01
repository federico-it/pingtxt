"use client"

import { useState } from "react"
import Link from "next/link"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { BrowserNotifyCard } from "@/components/browser-notify-card"
import { covers, EventChips, LinkRow, linkState, useEventList } from "@/components/channel-links"
import { Confirm } from "@/components/confirm"
import { GroupToggle, TagToggle, useLabels } from "@/components/labels"
import { Empty, PageHeader } from "@/components/page-header"
import { PeriodicReports } from "@/components/reports"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  api,
  defaultNotifyEvents,
  useApi,
  useIsAdmin,
  type ChannelLink,
  type Monitor,
  type Notifier,
  type NotifierType,
  type NotifyEvent,
  type PushSettings,
} from "@/lib/api"
import type en from "@/messages/en.json"

type FieldLabel = keyof (typeof en)["notifications"]["fields"]

type Field = {
  key: string
  /** Message key in notifications.fields. */
  label: FieldLabel
  placeholder?: string
  secret?: boolean
  // writeOnly secrets are never returned by the api: empty keeps the saved value.
  writeOnly?: boolean
  /** Labels are in notifications.options.<value>. */
  options?: string[]
  half?: boolean
  required?: boolean
  hint?: "commaSeparated" | "appUsers" | "resendKey"
  /** Shown only when this returns true for the current config. */
  when?: (config: Record<string, string>) => boolean
}

const isResend = (c: Record<string, string>) => c.provider === "resend"
const isSMTP = (c: Record<string, string>) => !isResend(c)

// Kind labels are in notifications.kinds.<type>.
const kinds: Record<NotifierType, { fields: Field[] }> = {
  webhook: { fields: [{ key: "url", label: "url", placeholder: "https://example.com/hooks/pingtxt" }] },
  discord: { fields: [{ key: "url", label: "webhookUrl", placeholder: "https://discord.com/api/webhooks/...", secret: true }] },
  slack: { fields: [{ key: "url", label: "webhookUrl", placeholder: "https://hooks.slack.com/services/...", secret: true }] },
  telegram: {
    fields: [
      { key: "bot_token", label: "botToken", placeholder: "123456:ABC...", secret: true },
      { key: "chat_id", label: "chatId", placeholder: "-1001234567890" },
    ],
  },
  ntfy: {
    fields: [
      { key: "url", label: "topicUrl", placeholder: "https://ntfy.sh/my-alerts" },
      { key: "token", label: "accessToken", placeholder: "tk_...", secret: true },
    ],
  },
  email: {
    fields: [
      { key: "provider", label: "provider", options: ["smtp", "resend"] },
      { key: "host", label: "smtpHost", placeholder: "smtp.example.com", required: true, half: true, when: isSMTP },
      { key: "port", label: "port", placeholder: "587", half: true, when: isSMTP },
      {
        key: "security",
        label: "security",
        options: ["starttls", "tls", "none"],
        when: isSMTP,
      },
      { key: "username", label: "username", placeholder: "alerts@example.com", half: true, when: isSMTP },
      { key: "password", label: "password", secret: true, writeOnly: true, half: true, when: isSMTP },
      { key: "api_key", label: "apiKey", placeholder: "re_...", secret: true, writeOnly: true, hint: "resendKey", when: isResend },
      { key: "from", label: "from", placeholder: "pingtxt <alerts@example.com>", required: true },
      { key: "to", label: "to", placeholder: "ops@example.com, oncall@example.com", required: true, hint: "commaSeparated" },
    ],
  },
  // Phones signed in with the pingtxt app, see Settings → mobile app.
  app: { fields: [{ key: "users", label: "users", placeholder: "alice@example.com, bob@example.com", hint: "appUsers" }] },
}

const portFor: Record<string, string> = { starttls: "587", tls: "465", none: "25" }

type Draft = {
  id?: number
  name: string
  type: NotifierType
  config: Record<string, string>
  enabled: boolean
  delay_min: number
  repeat_min: number
  all_monitors: boolean
  group_ids: number[]
  tag_ids: number[]
  events: NotifyEvent[]
  monitors: ChannelLink[]
}

const newDraft = (): Draft => ({
  name: "",
  type: "webhook",
  config: {},
  enabled: true,
  delay_min: 0,
  repeat_min: 0,
  all_monitors: true,
  group_ids: [],
  tag_ids: [],
  events: [...defaultNotifyEvents],
  monitors: [],
})

// Same limits as the hub: repeat at least every 5 minutes, at most one week.
const minRepeat = 5
const maxMinutes = 7 * 24 * 60

const minutes = (v: string) => Math.min(maxMinutes, Math.max(0, Math.trunc(Number(v)) || 0))

export default function NotificationsPage() {
  const { data, mutate } = useApi<Notifier[]>("/api/notifiers", { refreshInterval: 0 })
  const isAdmin = useIsAdmin()
  const t = useTranslations("notifications")
  const tl = useTranslations("channelLinks")
  const eventList = useEventList()
  const [draft, setDraft] = useState<Draft | null>(null)
  const { data: monitors } = useApi<Monitor[]>("/api/monitors", { refreshInterval: 0 })
  const { groups, tags } = useLabels()
  // App channels need push on in Settings (admins only can read it).
  const { data: push } = useApi<PushSettings>(isAdmin ? "/api/settings/push" : null, { refreshInterval: 0 })
  const pushOff = !!push && !(push.enabled && push.registered)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    if (!draft) return
    try {
      await api(draft.id ? `/api/notifiers/${draft.id}` : "/api/notifiers", { method: draft.id ? "PUT" : "POST", body: draft })
      toast.success(t("saved"))
      setDraft(null)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function test(n: Notifier) {
    const id = toast.loading(t("sendingTo", { name: n.name }))
    try {
      await api(`/api/notifiers/${n.id}/test`, { method: "POST" })
      toast.success(t("testSent"), { id })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"), { id })
    }
  }

  async function testDraft() {
    if (!draft) return
    const id = toast.loading(t("sending"))
    try {
      await api("/api/notifiers/test", { method: "POST", body: draft })
      toast.success(t("testSent"), { id })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"), { id })
    }
  }

  async function toggle(n: Notifier, enabled: boolean) {
    try {
      await api(`/api/notifiers/${n.id}`, { method: "PUT", body: { ...n, enabled } })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
    mutate()
  }

  async function remove(n: Notifier) {
    await api(`/api/notifiers/${n.id}`, { method: "DELETE" })
    mutate()
  }

  const kind = draft ? kinds[draft.type] : null

  function toggleID(k: "group_ids" | "tag_ids", id: number, on: boolean) {
    setDraft((p) => p && { ...p, [k]: on ? [...p[k], id] : p[k].filter((x) => x !== id) })
  }

  /** Sets the link of one monitor (undefined = follow the scope). */
  function setLink(monitorID: number, events: NotifyEvent[] | undefined) {
    setDraft((p) => {
      if (!p) return p
      const rest = p.monitors.filter((l) => l.monitor_id !== monitorID)
      return { ...p, monitors: events ? [...rest, { monitor_id: monitorID, events }] : rest }
    })
  }

  /** "all monitors", or how many monitors the channel hears about. */
  function scopeSummary(n: Notifier) {
    const links = n.monitors ?? []
    if (n.all_monitors && !links.some((l) => l.events.length === 0)) return tl("allMonitors")
    if (!monitors) return ""
    const count = monitors.filter((m) => linkState(covers(n, m), n.events ?? [], links.find((l) => l.monitor_id === m.id)?.events).on).length
    return tl("monitorCount", { count })
  }

  function setConfig(patch: Record<string, string>) {
    setDraft((p) => {
      if (!p) return p
      const config = { ...p.config, ...patch }
      // Follow the security mode with the default port unless a custom one is set.
      if (patch.security && (!p.config.port || p.config.port === portFor[p.config.security ?? "starttls"])) {
        config.port = portFor[patch.security] ?? ""
      }
      return { ...p, config }
    })
  }

  return (
    <>
      <PageHeader cmd="pingtxt notify ls" title={t("title")}>
        {isAdmin && (
          <Button onClick={() => setDraft(newDraft())}>
            <PlusIcon /> {t("add")}
          </Button>
        )}
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        {t("intro")}
      </p>

      <BrowserNotifyCard />

      {data && data.length === 0 ? (
        <Empty>{t("empty")}</Empty>
      ) : (
        <div className="border-t border-dashed">
          {(data ?? []).map((n) => (
            <div key={n.id} className="flex flex-wrap items-center gap-3 border-b border-dashed px-2 py-2.5">
              <Switch checked={n.enabled} onCheckedChange={(v) => toggle(n, v)} disabled={!isAdmin || (n.type === "app" && pushOff && !n.enabled)} />
              <div className="min-w-0 flex-1">
                <div className="font-medium">{n.name}</div>
                <div className="text-xs text-muted-foreground">
                  {[
                    kinds[n.type] ? t(`kinds.${n.type}`) : n.type,
                    n.type === "email" && (isResend(n.config) ? "resend" : "smtp"),
                    n.delay_min > 0 && t("afterDown", { min: n.delay_min }),
                    n.repeat_min > 0 && t("everyMin", { min: n.repeat_min }),
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </div>
                <div className="text-xs text-muted-foreground">
                  {[scopeSummary(n), eventList(n.events ?? [])]
                    .filter(Boolean)
                    .join(" · ")}
                </div>
                {n.type === "app" && pushOff && <PushOffNote />}
              </div>
              {isAdmin && (
                <>
                  <Button variant="ghost" size="xs" onClick={() => test(n)}>
                    {t("test")}
                  </Button>
                  <Button variant="ghost" size="xs" onClick={() => setDraft({ ...newDraft(), ...n, monitors: n.monitors ?? [] })}>
                    {t("edit")}
                  </Button>
                  <Confirm
                    title={t("deleteTitle", { name: n.name })}
                    onConfirm={() => remove(n)}
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

      {data && isAdmin !== undefined && <PeriodicReports notifiers={data} isAdmin={isAdmin} />}

      <Dialog open={!!draft} onOpenChange={(o) => !o && setDraft(null)}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
          <form onSubmit={save} className="grid gap-4">
            <DialogHeader>
              <DialogTitle>{draft?.id ? t("editTitle") : t("add")}</DialogTitle>
              <DialogDescription>{t("dialogText")}</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5">
              <Label>{t("type")}</Label>
              <Select
                items={(Object.keys(kinds) as NotifierType[]).map((value) => ({ value, label: t(`kinds.${value}`) }))}
                value={draft?.type}
                onValueChange={(v) =>
                  v && setDraft((p) => p && { ...p, type: v as NotifierType, config: v === "email" ? { security: "starttls", port: "587" } : ({} as Record<string, string>) })
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(kinds) as NotifierType[]).map((value) => (
                    <SelectItem key={value} value={value}>
                      {t(`kinds.${value}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="n-name">{t("name")}</Label>
              <Input id="n-name" required value={draft?.name ?? ""} onChange={(e) => setDraft((p) => p && { ...p, name: e.target.value })} placeholder={t("namePlaceholder")} />
            </div>
            {draft?.type === "app" && pushOff && <PushOffNote box />}
            <div className="grid grid-cols-2 gap-4">
              {kind?.fields
                .filter((f) => !f.when || f.when(draft?.config ?? {}))
                .map((f) => (
                  <NotifierField key={`${draft?.type}-${f.key}`} field={f} config={draft?.config ?? {}} onChange={setConfig} />
                ))}
            </div>
            <div className="grid grid-cols-2 gap-4 border-t border-dashed pt-4">
              <div className="col-span-2 grid content-start gap-1.5 sm:col-span-1">
                <Label htmlFor="n-delay">{t("delayMin")}</Label>
                <Input id="n-delay" type="number" min={0} max={maxMinutes} value={draft?.delay_min ?? 0} onChange={(e) => setDraft((p) => p && { ...p, delay_min: minutes(e.target.value) })} />
                <p className="text-xs text-muted-foreground">{t("delayHint")}</p>
              </div>
              <div className="col-span-2 grid content-start gap-1.5 sm:col-span-1">
                <Label htmlFor="n-repeat">{t("repeatMin")}</Label>
                <Input id="n-repeat" type="number" min={0} max={maxMinutes} value={draft?.repeat_min ?? 0} onChange={(e) => setDraft((p) => p && { ...p, repeat_min: minutes(e.target.value) })} />
                <p className="text-xs text-muted-foreground">{t("repeatHint", { min: minRepeat })}</p>
              </div>
            </div>
            {draft && (
              <div className="grid gap-4 border-t border-dashed pt-4">
                <div className="grid gap-1.5">
                  <Label>{tl("events")}</Label>
                  <EventChips value={draft.events} onChange={(events) => setDraft((p) => p && { ...p, events })} />
                  <p className="text-xs text-muted-foreground">{tl("eventsHint")}</p>
                </div>
                <div className="grid gap-2">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <Label>{tl("monitors")}</Label>
                    <label className="flex cursor-pointer items-center gap-2 text-xs">
                      <Switch checked={draft.all_monitors} onCheckedChange={(v) => setDraft((p) => p && { ...p, all_monitors: v })} />
                      {tl("allMonitors")}
                    </label>
                  </div>
                  {!draft.all_monitors && (groups.data?.length ?? 0) > 0 && (
                    <div className="flex flex-wrap items-center gap-1">
                      <span className="mr-1 text-xs text-muted-foreground">{tl("groups")}</span>
                      {(groups.data ?? []).map((g) => (
                        <GroupToggle key={g.id} group={g} on={draft.group_ids.includes(g.id)} onChange={(on) => toggleID("group_ids", g.id, on)} />
                      ))}
                    </div>
                  )}
                  {!draft.all_monitors && (tags.data?.length ?? 0) > 0 && (
                    <div className="flex flex-wrap items-center gap-1">
                      <span className="mr-1 text-xs text-muted-foreground">{tl("tags")}</span>
                      {(tags.data ?? []).map((tg) => (
                        <TagToggle key={tg.id} tag={tg} on={draft.tag_ids.includes(tg.id)} onChange={(on) => toggleID("tag_ids", tg.id, on)} />
                      ))}
                    </div>
                  )}
                  <p className="text-xs text-muted-foreground">{draft.all_monitors ? tl("allHint") : tl("scopeHint")}</p>
                  <div className="max-h-56 overflow-y-auto border border-dashed text-sm">
                    {(monitors ?? []).map((m) => {
                      const covered = covers(draft, m)
                      const link = draft.monitors.find((l) => l.monitor_id === m.id)
                      return (
                        <LinkRow
                          key={m.id}
                          label={m.name}
                          state={linkState(covered, draft.events, link?.events)}
                          covered={covered}
                          defaults={draft.events}
                          onChange={(events) => setLink(m.id, events)}
                        />
                      )
                    })}
                    {monitors?.length === 0 && <p className="px-2 py-1.5 text-xs text-muted-foreground">{tl("noMonitorsYet")}</p>}
                  </div>
                </div>
              </div>
            )}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={testDraft}>
                {t("sendTest")}
              </Button>
              <Button type="submit" disabled={draft?.type === "app" && pushOff && draft.enabled}>
                {t("save")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}

/** App channels cannot be enabled while push is off in Settings. */
function PushOffNote({ box }: { box?: boolean }) {
  const t = useTranslations("notifications")
  return (
    <p className={box ? "border border-dashed border-warn/40 p-3 text-xs text-warn" : "mt-0.5 text-xs text-warn"}>
      {t.rich("pushOff", {
        settings: (chunks) => (
          <Link href="/settings/integrations/#mobile-app" className="underline underline-offset-2">
            {chunks}
          </Link>
        ),
      })}
    </p>
  )
}

function NotifierField({ field: f, config, onChange }: { field: Field; config: Record<string, string>; onChange: (patch: Record<string, string>) => void }) {
  const id = `n-${f.key}`
  const saved = f.writeOnly && config[`${f.key}_set`] === "true"
  const t = useTranslations("notifications")
  const options = f.options?.map((o) => ({ value: o, label: t(`options.${o as "tls"}`) }))
  return (
    <div className={`grid gap-1.5 ${f.half ? "col-span-2 sm:col-span-1" : "col-span-2"}`}>
      <Label htmlFor={id}>
        {t(`fields.${f.label}`)}
        {saved && <span className="font-normal text-muted-foreground">{t("savedKeep")}</span>}
      </Label>
      {options ? (
        <Select items={options} value={config[f.key] ?? options[0].value} onValueChange={(v) => v && onChange({ [f.key]: v })}>
          <SelectTrigger id={id} className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {options.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : (
        <Input
          id={id}
          type={f.secret ? "password" : "text"}
          autoComplete={f.secret ? "new-password" : "off"}
          required={f.required}
          value={config[f.key] ?? ""}
          onChange={(e) => onChange({ [f.key]: e.target.value })}
          placeholder={saved ? "••••••••" : f.placeholder}
        />
      )}
      {f.hint === "resendKey" ? (
        <p className="text-xs text-muted-foreground">
          {t.rich("hints.resendKey", {
            link: (chunks) => (
              <a href="https://resend.com/api-keys" target="_blank" rel="noreferrer" className="underline underline-offset-2">
                {chunks}
              </a>
            ),
          })}
        </p>
      ) : (
        f.hint && <p className="text-xs text-muted-foreground">{t(`hints.${f.hint}`)}</p>
      )}
    </div>
  )
}
