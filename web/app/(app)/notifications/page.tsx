"use client"

import { useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Empty, PageHeader } from "@/components/page-header"
import { PeriodicReports } from "@/components/reports"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { api, useApi, useIsAdmin, type Notifier, type NotifierType } from "@/lib/api"
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
  hint?: "commaSeparated"
}

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
      { key: "host", label: "smtpHost", placeholder: "smtp.example.com", required: true, half: true },
      { key: "port", label: "port", placeholder: "587", half: true },
      {
        key: "security",
        label: "security",
        options: ["starttls", "tls", "none"],
      },
      { key: "username", label: "username", placeholder: "alerts@example.com", half: true },
      { key: "password", label: "password", secret: true, writeOnly: true, half: true },
      { key: "from", label: "from", placeholder: "pingtxt <alerts@example.com>", required: true },
      { key: "to", label: "to", placeholder: "ops@example.com, oncall@example.com", required: true, hint: "commaSeparated" },
    ],
  },
}

const portFor: Record<string, string> = { starttls: "587", tls: "465", none: "25" }

type Draft = { id?: number; name: string; type: NotifierType; config: Record<string, string>; enabled: boolean }

export default function NotificationsPage() {
  const { data, mutate } = useApi<Notifier[]>("/api/notifiers", { refreshInterval: 0 })
  const isAdmin = useIsAdmin()
  const t = useTranslations("notifications")
  const [draft, setDraft] = useState<Draft | null>(null)

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
    await api(`/api/notifiers/${n.id}`, { method: "PUT", body: { ...n, enabled } })
    mutate()
  }

  async function remove(n: Notifier) {
    await api(`/api/notifiers/${n.id}`, { method: "DELETE" })
    mutate()
  }

  const kind = draft ? kinds[draft.type] : null

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
          <Button onClick={() => setDraft({ name: "", type: "webhook", config: {}, enabled: true })}>
            <PlusIcon /> {t("add")}
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
          {(data ?? []).map((n) => (
            <div key={n.id} className="flex flex-wrap items-center gap-3 border-b border-dashed px-2 py-2.5">
              <Switch checked={n.enabled} onCheckedChange={(v) => toggle(n, v)} disabled={!isAdmin} />
              <div className="min-w-0 flex-1">
                <div className="font-medium">{n.name}</div>
                <div className="text-xs text-muted-foreground">{kinds[n.type] ? t(`kinds.${n.type}`) : n.type}</div>
              </div>
              {isAdmin && (
                <>
                  <Button variant="ghost" size="xs" onClick={() => test(n)}>
                    {t("test")}
                  </Button>
                  <Button variant="ghost" size="xs" onClick={() => setDraft({ ...n })}>
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
        <DialogContent className="sm:max-w-md">
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
            <div className="grid grid-cols-2 gap-4">
              {kind?.fields.map((f) => (
                <NotifierField key={`${draft?.type}-${f.key}`} field={f} config={draft?.config ?? {}} onChange={setConfig} />
              ))}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={testDraft}>
                {t("sendTest")}
              </Button>
              <Button type="submit">{t("save")}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
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
      {f.hint && <p className="text-xs text-muted-foreground">{t(`hints.${f.hint}`)}</p>}
    </div>
  )
}
