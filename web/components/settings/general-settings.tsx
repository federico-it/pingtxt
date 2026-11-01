"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { SaveBar } from "@/components/settings/save-bar"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  setTelemetry,
  TELEMETRY_DOCS,
  useTelemetry,
} from "@/components/telemetry-notice"
import { UPGRADE_DOCS } from "@/components/update-notice"
import {
  api,
  useApi,
  type GeneralSettings,
  type IPFamily,
  type Settings,
  type VersionStatus,
} from "@/lib/api"
import { ago } from "@/lib/format"

const ipFamilies = [
  [1, "either"],
  [4, "v4"],
  [6, "v6"],
  [46, "both"],
] as const

/**
 * State and save for the "general" settings object. Each settings page edits
 * one part of it; save sends the whole object, the rest as the hub has it.
 */
function useGeneralForm(settings: Settings, onSaved: (s: Settings) => void) {
  const [g, setG] = useState<GeneralSettings>(settings.general)
  const [busy, setBusy] = useState(false)
  const t = useTranslations("generalSettings")
  const set = <K extends keyof GeneralSettings>(k: K, v: GeneralSettings[K]) =>
    setG((p) => ({ ...p, [k]: v }))
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

  return { g, setG, set, dirty, busy, save, t }
}

type FormProps = { settings: Settings; onSaved: (s: Settings) => void }

/** General: public URL, built-in location, history, update check. */
export function InstanceForm({ settings, onSaved }: FormProps) {
  const { g, setG, set, dirty, busy, save, t } = useGeneralForm(settings, onSaved)
  return (
    <form onSubmit={save}>
      <Section title={t("instance")} className="mt-0">
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
          <Field label={t("localEnabled")} hint={t("localEnabledHint")}>
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
      <SaveBar dirty={dirty} busy={busy} onDiscard={() => setG(settings.general)} />
    </form>
  )
}

/** Monitors: defaults for new monitors, degraded threshold, IP family, expiry alerts. */
export function MonitorDefaultsForm({ settings, onSaved }: FormProps) {
  const { g, setG, set, dirty, busy, save, t } = useGeneralForm(settings, onSaved)
  const tf = useTranslations("monitorForm.ipFamily")
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
  function discard() {
    setG(settings.general)
    setCertDays(settings.general.expiry.cert_days.join(", "))
    setDomainDays(settings.general.expiry.domain_days.join(", "))
  }
  return (
    <form onSubmit={save}>
      <Section title={t("defaults")} className="mt-0">
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
          <Field label={t("ipFamily")} hint={t("ipFamilyHint")}>
            <Select
              items={ipFamilies.map(([v, k]) => ({
                value: String(v),
                label: tf(k),
              }))}
              value={String(g.ip_family || 1)}
              onValueChange={(v) =>
                v && set("ip_family", Number(v) as Exclude<IPFamily, 0>)
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ipFamilies.map(([v, k]) => (
                  <SelectItem key={v} value={String(v)}>
                    {tf(k)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
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
      <SaveBar dirty={dirty} busy={busy} onDiscard={discard} />
    </form>
  )
}

/** Integrations: whether /metrics needs an API token, plus a scrape example. */
export function MetricsForm({ settings, onSaved }: FormProps) {
  const { g, setG, set, dirty, busy, save, t } = useGeneralForm(settings, onSaved)
  return (
    <form onSubmit={save}>
      <Section title={t("metrics")}>
        <div className="grid gap-4">
          <Field label={t("metricsPublic")} hint={t("metricsPublicHint")}>
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
      <SaveBar dirty={dirty} busy={busy} onDiscard={() => setG(settings.general)} />
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
      await v.mutate(
        await api<VersionStatus>("/api/version/check", { method: "POST" }),
        {
          revalidate: false,
        }
      )
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  let line: React.ReactNode
  if (s.check_disabled) line = t("off")
  else if (!s.check_enabled) line = t("devBuild")
  else if (s.error)
    line = <span className="text-down">{t("failed", { error: s.error })}</span>
  else if (!s.latest) line = t("notYet")
  else if (s.update_available)
    line = (
      <span className="text-info">
        {t("newer", { latest: s.latest.version })}{" "}
        <a
          href={s.latest.url}
          target="_blank"
          rel="noreferrer"
          className="underline underline-offset-4"
        >
          {t("notes")}
        </a>{" "}
        ·{" "}
        <a
          href={UPGRADE_DOCS}
          target="_blank"
          rel="noreferrer"
          className="underline underline-offset-4"
        >
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
        <Button
          type="button"
          size="xs"
          variant="outline"
          disabled={busy}
          onClick={checkNow}
        >
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

/** Anonymous usage stats: saved right away, apart from the form. */
export function UsageStats() {
  const t = useTranslations("telemetry")
  const tg = useTranslations("generalSettings")
  const s = useTelemetry(true)
  const [busy, setBusy] = useState(false)
  if (!s.data) return null
  const d = s.data

  async function toggle(enabled: boolean) {
    setBusy(true)
    try {
      await s.mutate(await setTelemetry(enabled), { revalidate: false })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Section title={t("title")}>
      <div className="grid gap-4">
        <Field label={t("label")} hint={t("hint")}>
          <label className="flex h-8 items-center gap-2 text-xs">
            <Switch
              checked={d.enabled}
              disabled={busy || d.forced_off}
              onCheckedChange={toggle}
            />
            {d.enabled ? tg("enabled") : tg("disabled")}
          </label>
        </Field>
        <div className="grid gap-1 text-xs text-muted-foreground">
          {d.forced_off ? (
            <span>{t("forcedOff")}</span>
          ) : d.report ? (
            <>
              <span>{t("sends")}</span>
              <code className="rounded border border-dashed px-2 py-1 break-all">
                {JSON.stringify(d.report)}
              </code>
              <span>
                {d.error ? (
                  <span className="text-down">
                    {t("failed", { error: d.error })}
                  </span>
                ) : d.sent_at > 0 ? (
                  t("sent", { ago: ago(d.sent_at) })
                ) : (
                  t("notYet")
                )}
              </span>
            </>
          ) : (
            <span>{t("off")}</span>
          )}
          <a
            href={TELEMETRY_DOCS}
            target="_blank"
            rel="noreferrer"
            className="underline underline-offset-4 hover:text-foreground"
          >
            {t("what")}
          </a>
        </div>
      </div>
    </Section>
  )
}
