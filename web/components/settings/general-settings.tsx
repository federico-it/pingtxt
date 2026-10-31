"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { UPGRADE_DOCS } from "@/components/update-notice"
import {
  api,
  useApi,
  type GeneralSettings,
  type Settings,
  type VersionStatus,
} from "@/lib/api"
import { ago } from "@/lib/format"

export function GeneralSettingsForm({
  settings,
  onSaved,
}: {
  settings: Settings
  onSaved: (s: Settings) => void
}) {
  const [g, setG] = useState<GeneralSettings>(settings.general)
  const [busy, setBusy] = useState(false)
  const t = useTranslations("generalSettings")
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
      toast.success(t("saved"))
      onSaved(s)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save}>
      <Section title={t("instance")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={t("publicUrl")}
            htmlFor="public_url"
            className="sm:col-span-2"
            hint={t.rich("publicUrlHint", {
              url: settings.detected_url,
              hl: (c) => <span className="text-foreground">{c}</span>,
            })}
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
                <span>{t("missingScheme")}</span>
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
            label={t("localName")}
            htmlFor="local_name"
            hint={t("localNameHint")}
          >
            <Input
              id="local_name"
              value={g.local_name}
              onChange={(e) => set("local_name", e.target.value)}
            />
          </Field>
          <Field
            label={t("localEnabled")}
            hint={t("localEnabledHint")}
          >
            <label className="flex h-8 items-center gap-2 text-xs">
              <Switch
                checked={g.local_enabled}
                onCheckedChange={(v) => set("local_enabled", v)}
              />
              {g.local_enabled ? t("enabled") : t("disabled")}
            </label>
          </Field>
          <Field
            label={t("retention")}
            htmlFor="retention"
            hint={t("retentionHint")}
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
            label={t("rawRetention")}
            htmlFor="raw_retention"
            hint={t("rawRetentionHint")}
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

      <Section title={t("expiry")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={t("certDays")}
            htmlFor="cert_days"
            hint={t("certDaysHint")}
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
            label={t("domainDays")}
            htmlFor="domain_days"
            hint={t("domainDaysHint")}
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

      <Section title={t("metrics")}>
        <div className="grid gap-4">
          <Field
            label={t("metricsPublic")}
            hint={t("metricsPublicHint")}
          >
            <label className="flex h-8 items-center gap-2 text-xs">
              <Switch
                checked={g.metrics.public}
                onCheckedChange={(v) => set("metrics", { public: v })}
              />
              {g.metrics.public ? t("public") : t("tokenRequired")}
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

      <Section title={t("status")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={t("degradedPct")}
            htmlFor="degraded_pct"
            hint={t("degradedPctHint")}
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

      <Section title={t("updates")}>
        <div className="grid gap-4">
          <Field label={t("updateCheck")} hint={t("updateCheckHint")}>
            <label className="flex h-8 items-center gap-2 text-xs">
              <Switch
                checked={!g.update_check.disabled}
                onCheckedChange={(v) => set("update_check", { disabled: !v })}
              />
              {g.update_check.disabled ? t("disabled") : t("enabled")}
            </label>
          </Field>
          <UpdateStatus />
        </div>
      </Section>

      <Section title={t("defaults")}>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label={t("interval")} htmlFor="d_interval">
            <Input
              id="d_interval"
              type="number"
              min={10}
              max={86400}
              value={g.monitor_defaults.interval_sec}
              onChange={(e) => setDef("interval_sec", Number(e.target.value))}
            />
          </Field>
          <Field label={t("timeout")} htmlFor="d_timeout">
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
          <Field label={t("retries")} htmlFor="d_retries">
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
            {busy ? "..." : t("save")}
          </Button>
          {dirty && (
            <span className="text-[11px] text-muted-foreground">
              {t("unsaved")}
            </span>
          )}
        </div>
      </Section>
    </form>
  )
}

/** Current version, latest release and a "check now" button. */
function UpdateStatus() {
  const t = useTranslations("update")
  const v = useApi<VersionStatus>("/api/version", { refreshInterval: 60_000 })
  const [busy, setBusy] = useState(false)
  if (!v.data) return null
  const s = v.data

  async function checkNow() {
    setBusy(true)
    try {
      await v.mutate(await api<VersionStatus>("/api/version/check", { method: "POST" }), {
        revalidate: false,
      })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  let line: React.ReactNode
  if (s.check_disabled) line = t("off")
  else if (!s.check_enabled) line = t("devBuild")
  else if (s.error) line = <span className="text-down">{t("failed", { error: s.error })}</span>
  else if (!s.latest) line = t("notYet")
  else if (s.update_available)
    line = (
      <span className="text-info">
        {t("newer", { latest: s.latest.version })}{" "}
        <a href={s.latest.url} target="_blank" rel="noreferrer" className="underline underline-offset-4">
          {t("notes")}
        </a>{" "}
        ·{" "}
        <a href={UPGRADE_DOCS} target="_blank" rel="noreferrer" className="underline underline-offset-4">
          {t("how")}
        </a>
      </span>
    )
  else line = <span className="text-up">{t("upToDate")}</span>

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 text-xs text-muted-foreground">
      <span>
        {t("running", { current: s.current || "dev" })} · {line}
        {s.checked_at > 0 && <> · {t("checked", { ago: ago(s.checked_at) })}</>}
      </span>
      {s.check_enabled && (
        <Button type="button" size="xs" variant="outline" disabled={busy} onClick={checkNow}>
          {busy ? "..." : t("checkNow")}
        </Button>
      )}
    </div>
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
