"use client"

import { useState } from "react"
import { useTranslations } from "use-intl"

import { Checkbox } from "@/components/ui/checkbox"
import { defaultNotifyEvents, notifyEvents, type Monitor, type Notifier, type NotifyEvent } from "@/lib/api"
import { cn } from "@/lib/utils"

// Which channels get which events about which monitors. A channel has a scope
// (every monitor, or some groups and tags) and the events it gets for it; a
// link between one monitor and one channel overrides both. These helpers are
// shared by the channel dialog (channel -> monitors) and the monitor form
// (monitor -> channels), which edit the same links.

type Scope = Pick<Notifier, "all_monitors" | "group_ids" | "tag_ids">
type Labels = Pick<Monitor, "group_id" | "tag_ids">

/** Whether the channel's scope includes the monitor. */
export function covers(n: Scope, m: Labels): boolean {
  if (n.all_monitors) return true
  if (m.group_id && n.group_ids.includes(m.group_id)) return true
  return m.tag_ids.some((t) => n.tag_ids.includes(t))
}

const sameEvents = (a: NotifyEvent[], b: NotifyEvent[]) => a.length === b.length && a.every((e) => b.includes(e))

/** Keeps known events, once each, in display order. */
export const cleanEvents = (list: NotifyEvent[]) => notifyEvents.filter((e) => list.includes(e))

export type LinkState = {
  on: boolean
  events: NotifyEvent[]
  /** The monitor has a link of its own that differs from the scope. */
  custom: boolean
}

/** What a channel sends about a monitor, from its scope and the monitor's link. */
export function linkState(covered: boolean, defaults: NotifyEvent[], link?: NotifyEvent[]): LinkState {
  if (link) return { on: link.length > 0, events: link, custom: true }
  return { on: covered, events: covered ? defaults : [], custom: false }
}

/**
 * The link to store for a choice: undefined when the scope already gives
 * the same result (no link needed), else the events ([] = left out).
 */
export function nextLink(covered: boolean, defaults: NotifyEvent[], on: boolean, events: NotifyEvent[]): NotifyEvent[] | undefined {
  if (!on) return covered ? [] : undefined
  const ev = cleanEvents(events.length > 0 ? events : defaults.length > 0 ? defaults : defaultNotifyEvents)
  if (covered && sameEvents(ev, defaults)) return undefined
  return ev
}

/** Toggle buttons for the events of a channel or link. */
export function EventChips({
  value,
  onChange,
  disabled,
  className,
}: {
  value: NotifyEvent[]
  onChange: (v: NotifyEvent[]) => void
  disabled?: boolean
  className?: string
}) {
  const t = useTranslations("channelLinks")
  return (
    <div className={cn("flex flex-wrap gap-1", className)}>
      {notifyEvents.map((e) => {
        const on = value.includes(e)
        return (
          <button
            key={e}
            type="button"
            aria-pressed={on}
            disabled={disabled}
            onClick={() => onChange(cleanEvents(on ? value.filter((x) => x !== e) : [...value, e]))}
            className={cn(
              "border border-dashed px-1.5 py-0.5 text-xs transition-colors disabled:opacity-50",
              on ? cn("border-solid", eventTone[e]) : "text-muted-foreground hover:text-foreground"
            )}
          >
            {t(`event.${e}`)}
          </button>
        )
      })}
    </div>
  )
}

// Full class names, so Tailwind keeps them.
const eventTone: Record<NotifyEvent, string> = {
  down: "border-down/50 bg-down/10 text-down",
  up: "border-up/50 bg-up/10 text-up",
  degraded: "border-warn/50 bg-warn/10 text-warn",
  cert_expiry: "border-info/50 bg-info/10 text-foreground",
  domain_expiry: "border-info/50 bg-info/10 text-foreground",
}

/** Short list of events, e.g. "down, up". */
export function useEventList() {
  const t = useTranslations("channelLinks")
  return (events: NotifyEvent[]) => (events.length === 0 ? t("noEvents") : events.map((e) => t(`short.${e}`)).join(", "))
}

/**
 * One monitor in a channel, or one channel in a monitor: a checkbox, the
 * events it sends, and a button to give it events of its own.
 */
export function LinkRow({
  label,
  note,
  state,
  covered,
  defaults,
  onChange,
  disabled,
}: {
  label: React.ReactNode
  /** Why the row is on or off, e.g. "all monitors". */
  note?: React.ReactNode
  state: LinkState
  covered: boolean
  defaults: NotifyEvent[]
  /** The new link (undefined = none). */
  onChange: (link: NotifyEvent[] | undefined) => void
  disabled?: boolean
}) {
  const t = useTranslations("channelLinks")
  const list = useEventList()
  const [open, setOpen] = useState(false)
  const differs = state.custom && (state.on !== covered || (state.on && !sameEvents(state.events, defaults)))
  return (
    <div className="border-b border-dashed px-2 py-1.5 last:border-b-0">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
          <Checkbox
            checked={state.on}
            disabled={disabled}
            onCheckedChange={(v) => {
              onChange(nextLink(covered, defaults, v, v ? (state.on ? state.events : defaults) : []))
              if (!v) setOpen(false)
            }}
          />
          <span className="truncate">{label}</span>
          {note && <span className="shrink-0 text-[11px] text-muted-foreground">{note}</span>}
        </label>
        {state.on && (
          <button
            type="button"
            disabled={disabled}
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
            className={cn(
              "max-w-full truncate text-xs underline-offset-2 hover:underline disabled:opacity-50",
              differs ? "text-info" : "text-muted-foreground"
            )}
            title={t("editEvents")}
          >
            {list(state.events)}
          </button>
        )}
        {!state.on && differs && <span className="text-xs text-muted-foreground">{t("leftOut")}</span>}
      </div>
      {open && state.on && (
        <div className="mt-1.5 flex flex-wrap items-center gap-2 pl-6">
          <EventChips value={state.events} onChange={(ev) => onChange(nextLink(covered, defaults, ev.length > 0, ev))} disabled={disabled} />
          {differs && covered && (
            <button
              type="button"
              className="text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
              onClick={() => {
                onChange(undefined)
                setOpen(false)
              }}
            >
              {t("reset")}
            </button>
          )}
        </div>
      )}
    </div>
  )
}
