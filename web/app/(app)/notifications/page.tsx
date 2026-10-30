"use client"

import { useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"

import { Confirm } from "@/components/confirm"
import { Empty, PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { api, useApi, type Notifier, type NotifierType } from "@/lib/api"

const kinds: Record<NotifierType, { label: string; fields: { key: string; label: string; placeholder: string; secret?: boolean }[] }> = {
  webhook: { label: "webhook (json)", fields: [{ key: "url", label: "url", placeholder: "https://example.com/hooks/pingtxt" }] },
  discord: { label: "discord", fields: [{ key: "url", label: "webhook url", placeholder: "https://discord.com/api/webhooks/...", secret: true }] },
  slack: { label: "slack", fields: [{ key: "url", label: "webhook url", placeholder: "https://hooks.slack.com/services/...", secret: true }] },
  telegram: {
    label: "telegram",
    fields: [
      { key: "bot_token", label: "bot token", placeholder: "123456:ABC...", secret: true },
      { key: "chat_id", label: "chat id", placeholder: "-1001234567890" },
    ],
  },
  ntfy: {
    label: "ntfy",
    fields: [
      { key: "url", label: "topic url", placeholder: "https://ntfy.sh/my-alerts" },
      { key: "token", label: "access token (optional)", placeholder: "tk_...", secret: true },
    ],
  },
}

type Draft = { id?: number; name: string; type: NotifierType; config: Record<string, string>; enabled: boolean }

export default function NotificationsPage() {
  const { data, mutate } = useApi<Notifier[]>("/api/notifiers", { refreshInterval: 0 })
  const [draft, setDraft] = useState<Draft | null>(null)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    if (!draft) return
    try {
      await api(draft.id ? `/api/notifiers/${draft.id}` : "/api/notifiers", { method: draft.id ? "PUT" : "POST", body: draft })
      toast.success("saved")
      setDraft(null)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  async function test(n: Notifier) {
    const id = toast.loading(`sending test to ${n.name}...`)
    try {
      await api(`/api/notifiers/${n.id}/test`, { method: "POST" })
      toast.success("test notification sent", { id })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error", { id })
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

  return (
    <>
      <PageHeader cmd="pingtxt notify ls" title="notifications">
        <Button onClick={() => setDraft({ name: "", type: "webhook", config: {}, enabled: true })}>
          <PlusIcon /> add channel
        </Button>
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        Every enabled channel receives a message when a monitor goes down and when it recovers.
      </p>

      {data && data.length === 0 ? (
        <Empty>no channels. you won&apos;t be alerted when something breaks.</Empty>
      ) : (
        <div className="border-t border-dashed">
          {(data ?? []).map((n) => (
            <div key={n.id} className="flex flex-wrap items-center gap-3 border-b border-dashed px-2 py-2.5">
              <Switch checked={n.enabled} onCheckedChange={(v) => toggle(n, v)} />
              <div className="min-w-0 flex-1">
                <div className="font-medium">{n.name}</div>
                <div className="text-xs text-muted-foreground">{kinds[n.type]?.label ?? n.type}</div>
              </div>
              <Button variant="ghost" size="xs" onClick={() => test(n)}>
                test
              </Button>
              <Button variant="ghost" size="xs" onClick={() => setDraft({ ...n })}>
                edit
              </Button>
              <Confirm
                title={`delete ${n.name}?`}
                onConfirm={() => remove(n)}
                trigger={
                  <Button variant="ghost" size="xs" className="text-down">
                    rm
                  </Button>
                }
              />
            </div>
          ))}
        </div>
      )}

      <Dialog open={!!draft} onOpenChange={(o) => !o && setDraft(null)}>
        <DialogContent className="sm:max-w-md">
          <form onSubmit={save} className="grid gap-4">
            <DialogHeader>
              <DialogTitle>{draft?.id ? "edit channel" : "add channel"}</DialogTitle>
              <DialogDescription>Use &quot;test&quot; after saving to check the configuration.</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5">
              <Label>type</Label>
              <Select
                items={Object.entries(kinds).map(([value, k]) => ({ value, label: k.label }))}
                value={draft?.type}
                onValueChange={(v) => v && setDraft((p) => p && { ...p, type: v as NotifierType, config: {} })}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(kinds).map(([value, k]) => (
                    <SelectItem key={value} value={value}>
                      {k.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="n-name">name</Label>
              <Input id="n-name" required value={draft?.name ?? ""} onChange={(e) => setDraft((p) => p && { ...p, name: e.target.value })} placeholder="ops channel" />
            </div>
            {kind?.fields.map((f) => (
              <div key={f.key} className="grid gap-1.5">
                <Label htmlFor={`n-${f.key}`}>{f.label}</Label>
                <Input
                  id={`n-${f.key}`}
                  type={f.secret ? "password" : "text"}
                  value={draft?.config[f.key] ?? ""}
                  onChange={(e) => setDraft((p) => p && { ...p, config: { ...p.config, [f.key]: e.target.value } })}
                  placeholder={f.placeholder}
                />
              </div>
            ))}
            <DialogFooter>
              <Button type="submit">save</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
