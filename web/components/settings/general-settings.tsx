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
  const [certDays, setCertDays] = useState(
    settings.general.expiry.cert_days.join(", ")
  )
  const [domainDays, setDomainDays] = useState(
    settings.general.expiry.domain_days.join(", ")
  )
  const setExpiry = (k: keyof GeneralSettings["expiry"], text: string) =>
    setG((p) => ({ ...p, expiry: { ...p.expiry, [k]: parseDays(text) } }))
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
            {missingScheme(g.public_url) && (
              <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] text-amber-600 dark:text-amber-400">
                <span>no http:// or https:// in front: pick one</span>
                {(["https", "http"] as const).map((sc) => (
                  <Button
                    key={sc}
                    type="button"
                    size="xs"
                    variant={
                      settings.detected_url.startsWith(sc + ":")
                        ? "default"
                        : "outline"
                    }
                    onClick={() =>
                      set("public_url", `${sc}://${g.public_url.trim()}`)
                    }
                  >
                    {sc}://
                  </Button>
                ))}
              </div>
            )}
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
            label="keep history (days)"
            htmlFor="retention"
            hint="hourly summaries for uptime, charts and status pages"
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
          <Field
            label="keep every check (days)"
            htmlFor="raw_retention"
            hint="single results, at least 2 days. older ones live on in the hourly history"
          >
            <Input
              id="raw_retention"
              type="number"
              min={2}
              max={3650}
              value={g.raw_retention_days}
              onChange={(e) =>
                set("raw_retention_days", Number(e.target.value))
              }
            />
          </Field>
        </div>
      </Section>

      <Section title="expiry alerts">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="tls certificate (days before)"
            htmlFor="cert_days"
            hint="one alert as each threshold is crossed, plus one when it expires. empty: off"
          >
            <Input
              id="cert_days"
              placeholder="14, 7, 3, 1"
              value={certDays}
              onChange={(e) => {
                setCertDays(e.target.value)
                setExpiry("cert_days", e.target.value)
              }}
            />
          </Field>
          <Field
            label="domain registration (days before)"
            htmlFor="domain_days"
            hint="looked up with rdap, or whois where rdap is missing (e.g. .it)"
          >
            <Input
              id="domain_days"
              placeholder="30, 14, 7, 1"
              value={domainDays}
              onChange={(e) => {
                setDomainDays(e.target.value)
                setExpiry("domain_days", e.target.value)
              }}
            />
          </Field>
        </div>
      </Section>

      <Section title="prometheus metrics">
        <div className="grid gap-4">
          <Field
            label="public /metrics"
            hint="off: scrapers need an api token (read scope is enough). on: anyone who can reach the hub can read monitor names and statuses"
          >
            <label className="flex h-8 items-center gap-2 text-xs">
              <Switch
                checked={g.metrics.public}
                onCheckedChange={(v) => set("metrics", { public: v })}
              />
              {g.metrics.public ? "public" : "api token required"}
            </label>
          </Field>
          <pre className="overflow-x-auto border border-dashed p-3 text-[11px] leading-relaxed text-muted-foreground">
            {scrapeConfig(
              settings.general.public_url || settings.detected_url,
              g.metrics.public
            )}
          </pre>
        </div>
      </Section>

      <Section title="status">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="degraded at (% of locations)"
            htmlFor="degraded_pct"
            hint="degraded when at least this share of a monitor's online locations fails, down when more than half do. monitors can override it"
          >
            <Input
              id="degraded_pct"
              type="number"
              min={1}
              max={50}
              value={g.degraded_pct}
              onChange={(e) => set("degraded_pct", Number(e.target.value))}
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

/** A non-empty URL typed (or seeded) without http:// or https://. */
function missingScheme(u: string): boolean {
  const v = u.trim()
  return v !== "" && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v)
}

/** Example prometheus.yml job for this hub. */
function scrapeConfig(hubURL: string, isPublic: boolean): string {
  let u: URL
  try {
    u = new URL(hubURL)
  } catch {
    u = new URL("https://pingtxt.example.com")
  }
  const lines = [
    "scrape_configs:",
    "  - job_name: pingtxt",
    `    scheme: ${u.protocol.replace(":", "")}`,
    "    metrics_path: /metrics",
  ]
  if (!isPublic)
    lines.push("    authorization:", "      credentials: ptk_... # api token")
  lines.push("    static_configs:", `      - targets: ["${u.host}"]`)
  return lines.join("\n")
}

/** "30, 14 7" -> [30, 14, 7]: same order and limits as the hub. */
function parseDays(text: string): number[] {
  const days = text
    .split(/[\s,;]+/)
    .map(Number)
    .filter((n) => Number.isInteger(n) && n >= 1 && n <= 365)
  return [...new Set(days)].sort((a, b) => b - a).slice(0, 8)
}
