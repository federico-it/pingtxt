"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { EventChips } from "@/components/channel-links"
import { GroupToggle, TagToggle, useLabels } from "@/components/labels"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useNow } from "@/hooks/use-now"
import { api, useApi, type Monitor, type PushDevice } from "@/lib/api"
import { cn } from "@/lib/utils"

// Mute choices in minutes; 0 = until turned back on.
const muteChoices = ["60", "480", "1440", "0"] as const

const isMuted = (until: number, now: number) => until === -1 || until > now

/**
 * The phones of the current user that get app notifications, each with its
 * own events, monitors and mute. Personal: read-only users set them too, and
 * nothing changes for other people.
 */
export function MyPhones() {
  const { data, mutate } = useApi<PushDevice[]>("/api/mobile/devices", { refreshInterval: 0 })
  const t = useTranslations("myPhones")
  if (!data) return null
  if (data.length === 0) return <p className="text-xs text-muted-foreground">{t("none")}</p>
  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted-foreground">{t("hint")}</p>
      {data.map((d) => (
        <Phone key={`${d.id}-${JSON.stringify(d)}`} device={d} onSaved={() => mutate()} />
      ))}
    </div>
  )
}

function Phone({ device, onSaved }: { device: PushDevice; onSaved: () => void }) {
  const t = useTranslations("myPhones")
  const tl = useTranslations("channelLinks")
  const now = useNow()
  const { groups, tags, tree } = useLabels()
  const { data: monitors } = useApi<Monitor[]>("/api/monitors", { refreshInterval: 0 })
  const [draft, setDraft] = useState(device)
  const [mute, setMute] = useState<string>("60")
  const [busy, setBusy] = useState(false)
  const changed =
    JSON.stringify([draft.events, draft.all_monitors, draft.group_ids, draft.tag_ids, draft.monitor_ids]) !==
    JSON.stringify([device.events, device.all_monitors, device.group_ids, device.tag_ids, device.monitor_ids])
  const muted = isMuted(device.muted_until, now)
  const name = (id: number) => monitors?.find((m) => m.id === id)?.name ?? `#${id}`
  const mutedMonitors = device.muted_monitors.filter((m) => isMuted(m.until, now))
  const time = (ms: number) => new Date(ms).toLocaleString([], { dateStyle: "short", timeStyle: "short" })

  async function run(fn: () => Promise<unknown>, ok: string) {
    setBusy(true)
    try {
      await fn()
      toast.success(ok)
      onSaved()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }
  const path = `/api/mobile/devices/${device.id}`
  const save = () =>
    run(
      () =>
        api(path, {
          method: "PUT",
          body: {
            events: draft.events,
            all_monitors: draft.all_monitors,
            group_ids: draft.group_ids,
            tag_ids: draft.tag_ids,
            monitor_ids: draft.monitor_ids,
          },
        }),
      t("saved")
    )
  const toggle = (k: "group_ids" | "tag_ids" | "monitor_ids", id: number, on: boolean) =>
    setDraft((p) => ({ ...p, [k]: on ? [...p[k], id] : p[k].filter((x) => x !== id) }))

  return (
    <div className="grid gap-4 border border-dashed p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <span className="text-sm text-foreground">{device.name}</span>
          <span className="block text-[11px] text-muted-foreground">
            {t("lastSeen", { date: new Date(device.last_seen_at).toLocaleDateString() })}
          </span>
        </div>
        {muted ? (
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="text-warn">
              {device.muted_until === -1 ? t("mutedForever") : t("mutedUntil", { time: time(device.muted_until) })}
            </span>
            <Button size="xs" variant="outline" disabled={busy} onClick={() => run(() => api(`${path}/mute`, { method: "DELETE" }), t("unmuted"))}>
              {t("unmute")}
            </Button>
          </div>
        ) : (
          <div className="flex items-center gap-2">
            <Select items={muteChoices.map((v) => ({ value: v, label: t(`for.${v}`) }))} value={mute} onValueChange={(v) => v && setMute(v)}>
              <SelectTrigger size="sm" className="w-40" aria-label={t("muteFor")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {muteChoices.map((v) => (
                  <SelectItem key={v} value={v}>
                    {t(`for.${v}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button size="xs" variant="outline" disabled={busy} onClick={() => run(() => api(`${path}/mute`, { method: "POST", body: { minutes: Number(mute) } }), t("mutedToast"))}>
              {t("mute")}
            </Button>
          </div>
        )}
      </div>
      {mutedMonitors.length > 0 && (
        <div className="grid gap-1">
          <Label>{t("mutedMonitors")}</Label>
          {mutedMonitors.map((m) => (
            <div key={m.monitor_id} className="flex flex-wrap items-center justify-between gap-2 text-xs">
              <span>
                {name(m.monitor_id)}{" "}
                <span className="text-muted-foreground">· {m.until === -1 ? t("mutedForever") : t("mutedUntil", { time: time(m.until) })}</span>
              </span>
              <Button size="xs" variant="ghost" disabled={busy} onClick={() => run(() => api(`${path}/mute?monitor_id=${m.monitor_id}`, { method: "DELETE" }), t("unmuted"))}>
                {t("unmute")}
              </Button>
            </div>
          ))}
        </div>
      )}
      <div className="grid gap-1.5">
        <Label>{tl("events")}</Label>
        <EventChips value={draft.events} onChange={(events) => setDraft((p) => ({ ...p, events }))} />
        <p className="text-xs text-muted-foreground">{t("eventsHint")}</p>
      </div>
      <div className="grid gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Label>{tl("monitors")}</Label>
          <label className="flex cursor-pointer items-center gap-2 text-xs">
            <Switch checked={draft.all_monitors} onCheckedChange={(v) => setDraft((p) => ({ ...p, all_monitors: v }))} />
            {tl("allMonitors")}
          </label>
        </div>
        {!draft.all_monitors && (
          <>
            {(groups.data?.length ?? 0) > 0 && (
              <div className="flex flex-wrap items-center gap-1">
                <span className="mr-1 text-xs text-muted-foreground">{tl("groups")}</span>
                {tree.ordered.map((g) => (
                  <GroupToggle key={g.id} group={g} on={draft.group_ids.includes(g.id)} onChange={(on) => toggle("group_ids", g.id, on)} />
                ))}
              </div>
            )}
            {(tags.data?.length ?? 0) > 0 && (
              <div className="flex flex-wrap items-center gap-1">
                <span className="mr-1 text-xs text-muted-foreground">{tl("tags")}</span>
                {(tags.data ?? []).map((tg) => (
                  <TagToggle key={tg.id} tag={tg} on={draft.tag_ids.includes(tg.id)} onChange={(on) => toggle("tag_ids", tg.id, on)} />
                ))}
              </div>
            )}
            <div className="flex flex-wrap items-center gap-1">
              <span className="mr-1 text-xs text-muted-foreground">{t("alsoMonitors")}</span>
              {(monitors ?? []).map((m) => {
                const on = draft.monitor_ids.includes(m.id)
                return (
                  <button
                    key={m.id}
                    type="button"
                    aria-pressed={on}
                    onClick={() => toggle("monitor_ids", m.id, !on)}
                    className={cn(
                      "border border-dashed px-1.5 py-0.5 text-xs transition-colors",
                      on ? "border-solid border-info bg-info/15 text-foreground" : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    {m.name}
                  </button>
                )
              })}
            </div>
          </>
        )}
        <p className="text-xs text-muted-foreground">{draft.all_monitors ? t("allHint") : t("scopeHint")}</p>
      </div>
      <div className="flex justify-end">
        <Button size="sm" disabled={busy || !changed} onClick={save}>
          {t("save")}
        </Button>
      </div>
    </div>
  )
}
