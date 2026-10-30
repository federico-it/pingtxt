"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { api, type GeneralSettings, type Settings } from "@/lib/api"

export function GeneralSettingsForm({
  settings,
  onSaved,
}: {
  settings: Settings
  onSaved: (s: Settings) => void
}) {
  const [g, setG] = useState<GeneralSettings>(settings.general)
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof GeneralSettings>(k: K, v: GeneralSettings[K]) =>
    setG((p) => ({ ...p, [k]: v }))
  const setDef = (k: keyof GeneralSettings["monitor_defaults"], v: number) =>
    setG((p) => ({ ...p, monitor_defaults: { ...p.monitor_defaults, [k]: v } }))
  const dirty = JSON.stringify(g) !== JSON.stringify(settings.general)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const s = await api<Settings>("/api/settings/general", {
        method: "PUT",
        body: g,
      })
      toast.success("settings saved")
      onSaved(s)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save}>
      <Section title="instance">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="public url"
            htmlFor="public_url"
            className="sm:col-span-2"
            hint={
              <>
                used for agent install commands and login callbacks. empty:
                detected from the request (
                <span className="text-foreground">{settings.detected_url}</span>
                ).
              </>
            }
          >
            <Input
              id="public_url"
              type="url"
              placeholder="https://status.example.com"
              value={g.public_url}
              onChange={(e) => set("public_url", e.target.value)}
            />
          </Field>
          <Field
            label="built-in location"
            htmlFor="local_name"
            hint="name of the probe that runs inside the hub"
          >
            <Input
              id="local_name"
              value={g.local_name}
              onChange={(e) => set("local_name", e.target.value)}
            />
          </Field>
          <Field
            label="run checks from the hub"
            hint="off: checks run only from agents"
          >
            <label className="flex h-8 items-center gap-2 text-xs">
              <Switch
                checked={g.local_enabled}
                onCheckedChange={(v) => set("local_enabled", v)}
              />
              {g.local_enabled ? "enabled" : "disabled"}
            </label>
          </Field>
          <Field
            label="keep check results (days)"
            htmlFor="retention"
            hint="older raw results are deleted every hour"
          >
            <Input
              id="retention"
              type="number"
              min={1}
              max={3650}
              value={g.retention_days}
              onChange={(e) => set("retention_days", Number(e.target.value))}
            />
          </Field>
        </div>
      </Section>

      <Section title="new monitor defaults">
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="interval (s)" htmlFor="d_interval">
            <Input
              id="d_interval"
              type="number"
              min={10}
              max={86400}
              value={g.monitor_defaults.interval_sec}
              onChange={(e) => setDef("interval_sec", Number(e.target.value))}
            />
          </Field>
          <Field label="timeout (ms)" htmlFor="d_timeout">
            <Input
              id="d_timeout"
              type="number"
              min={500}
              max={60000}
              step={500}
              value={g.monitor_defaults.timeout_ms}
              onChange={(e) => setDef("timeout_ms", Number(e.target.value))}
            />
          </Field>
          <Field label="retries" htmlFor="d_retries">
            <Input
              id="d_retries"
              type="number"
              min={1}
              max={10}
              value={g.monitor_defaults.retries}
              onChange={(e) => setDef("retries", Number(e.target.value))}
            />
          </Field>
        </div>
        <div className="mt-4 flex items-center gap-3">
          <Button type="submit" variant="outline" disabled={busy || !dirty}>
            {busy ? "..." : "save"}
          </Button>
          {dirty && (
            <span className="text-[11px] text-muted-foreground">
              unsaved changes
            </span>
          )}
        </div>
      </Section>
    </form>
  )
}
