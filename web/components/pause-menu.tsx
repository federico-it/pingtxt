"use client"

import { useState, type ReactElement } from "react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useNow } from "@/hooks/use-now"
import { datetime, duration } from "@/lib/format"

/**
 * How long a pause lasts, as /api/monitors/{id}/pause and the bulk "pause"
 * action take it: minutes from now, until a time (unix ms), or neither
 * (until someone resumes the monitor).
 */
export type PauseLength = { minutes?: number; until?: number }

/** Pause lengths offered, in minutes. */
const pauseMinutes = [60, 720, 1440]

/** Longest pause the hub takes. */
const maxPauseMs = 365 * 24 * 3600 * 1000

/** "14:30" (today) or "2026-11-19 09:00", and whether it is today. */
export function untilText(ts: number) {
  const full = datetime(ts)
  const today = full.slice(0, 10) === datetime(Date.now()).slice(0, 10)
  return { until: today ? full.slice(11, 16) : full.slice(0, 16), today: today ? "yes" : "no" }
}

/**
 * "paused", or for a pause that ends by itself a short "paused → 14:30"
 * (today) / "paused → 11-20" and the full time as a tooltip.
 */
export function PausedText({ pausedUntil }: { pausedUntil?: number }) {
  const t = useTranslations("pause")
  const ts = useTranslations("status")
  const until = pausedUntil ?? 0
  if (until <= 0) return <>{ts("paused")}</>
  const u = untilText(until)
  const short = u.today === "yes" ? u.until : u.until.slice(5, 10)
  return <span title={t("pausedUntil", u)}>{t("pausedShort", { until: short })}</span>
}

/** One line on a paused monitor: when it resumes, or that it waits for someone. */
export function PauseNote({ pausedUntil, now }: { pausedUntil?: number; now: number }) {
  const t = useTranslations("pause")
  const until = pausedUntil ?? 0
  return (
    <div className="mb-4 border border-dashed px-3 py-2 text-xs text-muted-foreground">
      ○{" "}
      {until > 0
        ? t.rich("noteTimed", {
            ...untilText(until),
            left: duration(Math.max(0, until - now)),
            hl: (c) => <span className="text-foreground">{c}</span>,
          })
        : t("noteForever")}
    </div>
  )
}

// <input type="datetime-local"> works in the browser's time zone.
function toLocalInput(ts: number) {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

/**
 * A menu to pause for 1 hour, 12 hours, 24 hours, until a chosen time or
 * until resumed. The trigger is the button that opens it.
 */
export function PauseMenu({
  trigger,
  onPause,
  disabled,
}: {
  trigger: ReactElement
  onPause: (len: PauseLength) => void
  disabled?: boolean
}) {
  const t = useTranslations("pause")
  const [custom, setCustom] = useState(false)
  const [value, setValue] = useState("")
  const now = useNow()
  const picked = value ? new Date(value).getTime() : NaN
  const valid = !Number.isNaN(picked) && picked > now && picked <= now + maxPauseMs

  function openCustom() {
    setValue(toLocalInput(Date.now() + 2 * 3600 * 1000))
    setCustom(true)
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger disabled={disabled} render={trigger} />
        <DropdownMenuContent align="end" className="min-w-52">
          {pauseMinutes.map((m) => (
            <DropdownMenuItem key={m} onClick={() => onPause({ minutes: m })}>
              {t("for", { count: m / 60 })}
            </DropdownMenuItem>
          ))}
          <DropdownMenuItem onClick={openCustom}>{t("custom")}</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => onPause({})}>{t("forever")}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={custom} onOpenChange={setCustom}>
        <DialogContent className="sm:max-w-sm">
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault()
              if (!valid) return
              setCustom(false)
              onPause({ until: picked })
            }}
          >
            <DialogHeader>
              <DialogTitle>{t("customTitle")}</DialogTitle>
              <DialogDescription>{t("customText")}</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5">
              <Label htmlFor="pause-until">{t("resumeAt")}</Label>
              <Input
                id="pause-until"
                type="datetime-local"
                required
                min={toLocalInput(now)}
                value={value}
                onChange={(e) => setValue(e.target.value)}
              />
              {value && !valid && <p className="text-xs text-down">{t("invalid")}</p>}
              {valid && <p className="text-xs text-muted-foreground">{t("left", { left: duration(picked - now) })}</p>}
            </div>
            <DialogFooter>
              <Button type="submit" disabled={!valid}>
                {t("pause")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
