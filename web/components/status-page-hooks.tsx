"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { CopyBlock } from "@/components/copy-block"
import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import {
  api,
  pageWebhookEvents,
  useApi,
  type PageFeeds,
  type PageWebhook,
  type PageWebhookEvent,
} from "@/lib/api"
import { ago } from "@/lib/format"
import { cn } from "@/lib/utils"

/** RSS/Atom addresses and outgoing webhooks of a saved status page (admins). */
export function StatusPageHooks({ id }: { id: number }) {
  return (
    <div className="grid max-w-3xl grid-cols-[minmax(0,1fr)] gap-2">
      <Feeds id={id} />
      <Webhooks id={id} />
    </div>
  )
}

function Feeds({ id }: { id: number }) {
  const t = useTranslations("pageHooks")
  const { data } = useApi<PageFeeds>(`/api/status-pages/${id}/feeds`, {
    refreshInterval: 0,
  })
  if (!data) return null
  return (
    <Section title={t("feedsTitle")}>
      <p className="mb-3 text-xs text-muted-foreground">
        {data.private ? t("feedsPrivate") : t("feedsHint")}
        {!data.published && (
          <span className="block text-warn">{t("feedsDraft")}</span>
        )}
      </p>
      <div className="grid gap-3">
        <Field label="atom">
          <CopyBlock code={data.atom} />
        </Field>
        <Field label="rss">
          <CopyBlock code={data.rss} />
        </Field>
      </div>
    </Section>
  )
}

function EventPicker({
  value,
  onChange,
}: {
  value: PageWebhookEvent[]
  onChange: (v: PageWebhookEvent[]) => void
}) {
  const t = useTranslations("pageHooks")
  const all = value.length === 0
  return (
    <div className="grid gap-1.5">
      <label className="flex cursor-pointer items-center gap-2 text-xs">
        <Checkbox
          checked={all}
          onCheckedChange={(v) => onChange(v ? [] : [...pageWebhookEvents])}
        />
        {t("allEvents")}
      </label>
      {!all &&
        pageWebhookEvents.map((e) => (
          <label
            key={e}
            className="flex cursor-pointer items-center gap-2 pl-6 text-xs"
          >
            <Checkbox
              checked={value.includes(e)}
              onCheckedChange={(v) =>
                onChange(v ? [...value, e] : value.filter((x) => x !== e))
              }
            />
            <code>{e}</code>
            <span className="text-muted-foreground">{t(`event.${e}`)}</span>
          </label>
        ))}
    </div>
  )
}

function Webhooks({ id }: { id: number }) {
  const t = useTranslations("pageHooks")
  const base = `/api/status-pages/${id}/webhooks`
  const { data, mutate } = useApi<{ webhooks: PageWebhook[] }>(base, {
    refreshInterval: 30_000,
  })
  const [url, setUrl] = useState("")
  const [events, setEvents] = useState<PageWebhookEvent[]>([])
  const [busy, setBusy] = useState(false)
  if (!data) return null

  async function add(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await api(base, { method: "POST", body: { url, events } })
      toast.success(t("added"))
      setUrl("")
      setEvents([])
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Section title={t("webhooksTitle", { count: data.webhooks.length })}>
      <p className="mb-3 text-xs text-muted-foreground">{t("webhooksHint")}</p>
      {data.webhooks.length > 0 && (
        <div className="mb-4 border-t border-dashed">
          {data.webhooks.map((w) => (
            <WebhookRow
              key={`${w.id}-${w.secret}`}
              base={base}
              hook={w}
              onChange={() => mutate()}
            />
          ))}
        </div>
      )}
      {data.webhooks.length < 10 && (
        <form onSubmit={add} className="grid gap-3 border border-dashed p-4">
          <Field label={t("url")} htmlFor="wh-url">
            <Input
              id="wh-url"
              type="url"
              inputMode="url"
              required
              maxLength={2048}
              pattern="https?://.+"
              value={url}
              placeholder="https://example.com/hooks/status"
              onChange={(e) => setUrl(e.target.value)}
            />
          </Field>
          <EventPicker value={events} onChange={setEvents} />
          <div>
            <Button type="submit" disabled={busy}>
              {t("add")}
            </Button>
          </div>
        </form>
      )}
    </Section>
  )
}

function WebhookRow({
  base,
  hook,
  onChange,
}: {
  base: string
  hook: PageWebhook
  onChange: () => void
}) {
  const t = useTranslations("pageHooks")
  const [open, setOpen] = useState(false)
  const [events, setEvents] = useState<PageWebhookEvent[]>(hook.events)
  const [busy, setBusy] = useState(false)
  const path = `${base}/${hook.id}`

  async function put(body: Partial<PageWebhook>, done: string) {
    setBusy(true)
    try {
      await api(path, { method: "PUT", body })
      toast.success(done)
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  async function test() {
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; status: number; error?: string }>(
        `${path}/test`,
        { method: "POST" }
      )
      if (r.ok) toast.success(t("testOk", { status: r.status }))
      else toast.error(t("testFailed", { error: r.error ?? "" }))
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    try {
      await api(path, { method: "DELETE" })
      toast.success(t("removed"))
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  const failed = hook.last_at > 0 && hook.last_error !== ""
  return (
    <div className="border-b border-dashed px-2 py-2">
      <div className="grid gap-1 sm:flex sm:items-center sm:gap-3">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <Switch
            checked={hook.enabled}
            aria-label={t("enabled")}
            disabled={busy}
            onCheckedChange={(v) =>
              put({ enabled: v }, v ? t("turnedOn") : t("turnedOff"))
            }
          />
          <span
            className={cn(
              "min-w-0 flex-1 truncate",
              !hook.enabled && "text-muted-foreground"
            )}
          >
            {hook.url}
          </span>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-x-3">
          <span
            className={cn(
              "mr-auto text-xs sm:mr-0",
              failed
                ? "text-down"
                : hook.last_at
                  ? "text-up"
                  : "text-muted-foreground"
            )}
            title={hook.last_error || undefined}
          >
            {hook.last_at === 0
              ? t("neverSent")
              : failed
                ? t("lastFailed", {
                    when: ago(hook.last_at),
                    status: hook.last_status,
                  })
                : t("lastOk", {
                    when: ago(hook.last_at),
                    status: hook.last_status,
                  })}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            disabled={busy}
            onClick={test}
          >
            {t("test")}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={() => setOpen((o) => !o)}
          >
            {open ? t("close") : t("edit")}
          </Button>
          <Confirm
            title={t("removeTitle", { url: hook.url })}
            onConfirm={remove}
            trigger={
              <Button
                type="button"
                variant="ghost"
                size="xs"
                className="text-down"
              >
                {t("rm")}
              </Button>
            }
          />
        </div>
      </div>
      {failed && (
        <div className="mt-1 truncate text-xs text-down">
          ! {hook.last_error}
        </div>
      )}
      {open && (
        <div className="mt-3 grid gap-3 pl-1">
          <Field label={t("secret")} hint={t("secretHint")}>
            <CopyBlock code={hook.secret} />
          </Field>
          <div className="flex flex-wrap gap-2">
            <Confirm
              title={t("rotateTitle")}
              onConfirm={() => put({ secret: "" }, t("rotated"))}
              trigger={
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy}
                >
                  {t("rotate")}
                </Button>
              }
            />
          </div>
          <EventPicker value={events} onChange={setEvents} />
          <div>
            <Button
              type="button"
              size="sm"
              disabled={busy}
              onClick={() => put({ events }, t("saved"))}
            >
              {t("saveEvents")}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
