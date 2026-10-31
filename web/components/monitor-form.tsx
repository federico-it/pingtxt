"use client"

import { useRouter } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { TagPicker, useLabels } from "@/components/labels"
import { Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import {
  api,
  useApi,
  type Location,
  type Monitor,
  type MonitorType,
} from "@/lib/api"
import { interval } from "@/lib/format"
import { cn } from "@/lib/utils"

// Labels and hints are in messages: monitorForm.types.<value>.
const types: {
  value: MonitorType
  placeholder: string
  /** Label of the target field (monitorForm.target.<key>); defaults to "host". */
  target?: "url" | "container" | "address" | "server"
}[] = [
  {
    value: "http",
    placeholder: "https://example.com/health",
  },
  {
    value: "keyword",
    placeholder: "https://example.com",
  },
  {
    value: "tcp",
    placeholder: "db.example.com:5432",
  },
  {
    value: "ping",
    placeholder: "1.1.1.1",
  },
  {
    value: "dns",
    placeholder: "example.com",
  },
  {
    value: "postgres",
    placeholder: "postgres://user:password@db.example.com:5432/app",
    target: "url",
  },
  {
    value: "mysql",
    placeholder: "mysql://user:password@db.example.com:3306/app",
    target: "url",
  },
  {
    value: "redis",
    placeholder: "redis://:password@cache.example.com:6379/0",
    target: "url",
  },
  {
    value: "mongodb",
    placeholder:
      "mongodb://user:password@db.example.com:27017/?authSource=admin",
    target: "url",
  },
  {
    value: "docker",
    placeholder: "my-app-web-1",
    target: "container",
  },
  {
    value: "grpc",
    placeholder: "grpcs://api.example.com:443/my.Service",
    target: "address",
  },
  {
    value: "smtp",
    placeholder: "smtp://mail.example.com:587",
    target: "server",
  },
  {
    value: "push",
    placeholder: "",
  },
]

const intervals = [
  { value: "20", label: "20s" },
  { value: "30", label: "30s" },
  { value: "60", label: "1m" },
  { value: "120", label: "2m" },
  { value: "300", label: "5m" },
  { value: "600", label: "10m" },
  { value: "1800", label: "30m" },
  { value: "3600", label: "1h" },
]

const pushIntervals = [
  { value: "60", label: "1m" },
  { value: "300", label: "5m" },
  { value: "600", label: "10m" },
  { value: "900", label: "15m" },
  { value: "1800", label: "30m" },
  { value: "3600", label: "1h" },
  { value: "7200", label: "2h" },
  { value: "21600", label: "6h" },
  { value: "43200", label: "12h" },
  { value: "86400", label: "1d" },
  { value: "604800", label: "7d" },
]

const graces = [
  { value: "0", label: "" },
  { value: "60", label: "1m" },
  { value: "300", label: "5m" },
  { value: "900", label: "15m" },
  { value: "1800", label: "30m" },
  { value: "3600", label: "1h" },
  { value: "21600", label: "6h" },
]

/**
 * Adds the current value to a preset list so the select can show it, with
 * labels in the units of the current language.
 */
function withValue(list: { value: string; label: string }[], sec: number) {
  const all = list.some((x) => x.value === String(sec))
    ? list
    : [...list, { value: String(sec), label: "" }].sort(
        (a, b) => Number(a.value) - Number(b.value)
      )
  return all.map((x) => (x.value === "0" ? x : { ...x, label: interval(Number(x.value)) }))
}

const methods = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
const records = ["A", "AAAA", "CNAME", "MX", "TXT", "NS"]

export type MonitorInput = Omit<
  Monitor,
  "id" | "status" | "status_since" | "created_at" | "push_token"
>

export const emptyMonitor: MonitorInput = {
  name: "",
  type: "http",
  target: "",
  interval_sec: 60,
  timeout_ms: 10000,
  retries: 2,
  method: "GET",
  expected_status: "",
  keyword: "",
  keyword_invert: false,
  dns_record: "A",
  dns_server: "",
  dns_expected: "",
  ignore_tls: false,
  headers: {},
  body: "",
  grace_sec: 300,
  locations: ["local"],
  group_id: 0,
  tag_ids: [],
  cert_expiry_alert: true,
  domain_expiry_alert: true,
  paused: false,
}

function headersToText(h: Record<string, string>) {
  return Object.entries(h)
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n")
}

function textToHeaders(t: string) {
  const out: Record<string, string> = {}
  for (const line of t.split("\n")) {
    const i = line.indexOf(":")
    if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return out
}

function Field({
  label,
  hint,
  children,
  className,
}: {
  label: string
  hint?: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn("grid content-start gap-1.5", className)}>
      <Label>{label}</Label>
      {children}
      {hint && <p className="text-[11px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function MonitorForm({
  initial,
  id,
}: {
  initial: MonitorInput
  id?: number
}) {
  const router = useRouter()
  const [m, setM] = useState<MonitorInput>(initial)
  const [headers, setHeaders] = useState(headersToText(initial.headers))
  const [busy, setBusy] = useState(false)
  const locations = useApi<Location[]>("/api/locations", {
    refreshInterval: 15_000,
  })
  const labels = useLabels()
  const tr = useTranslations("monitorForm")
  const tc = useTranslations("common")
  const groupItems = [
    { value: "0", label: tr("noGroup") },
    ...(labels.groups.data ?? []).map((g) => ({
      value: String(g.id),
      label: g.name,
    })),
  ]

  const set = <K extends keyof MonitorInput>(k: K, v: MonitorInput[K]) =>
    setM((p) => ({ ...p, [k]: v }))
  const t = types.find((x) => x.value === m.type) ?? types[0]
  const isHTTP = m.type === "http" || m.type === "keyword"
  const isHTTPS = isHTTP && m.target.trim().toLowerCase().startsWith("https://")
  const isPush = m.type === "push"
  const isSQL = m.type === "postgres" || m.type === "mysql"
  const hasTLS = [
    "postgres",
    "mysql",
    "redis",
    "mongodb",
    "grpc",
    "smtp",
  ].includes(m.type)
  const ivList = isPush
    ? withValue(pushIntervals, m.interval_sec)
    : withValue(intervals, m.interval_sec)
  const graceList = withValue(graces, m.grace_sec).map((g) => (g.value === "0" ? { ...g, label: tr("noGrace") } : g))
  const typeItems = types.map((x) => ({ value: x.value, label: tr(`types.${x.value}.label`) }))

  function setType(type: MonitorType) {
    setM((p) => {
      const next = { ...p, type }
      if (type === "push" && p.type !== "push") {
        next.interval_sec = 3600
        if (!p.grace_sec) next.grace_sec = 300
      } else if (type !== "push" && p.type === "push") {
        next.interval_sec = 60
        if (!next.locations.some((l) => l !== "push"))
          next.locations = ["local"]
      }
      return next
    })
  }

  function toggleLoc(loc: string, on: boolean) {
    set(
      "locations",
      on
        ? [...new Set([...m.locations, loc])]
        : m.locations.filter((l) => l !== loc)
    )
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const body = { ...m, headers: textToHeaders(headers) }
      const res = await api<Monitor>(
        id ? `/api/monitors/${id}` : "/api/monitors",
        { method: id ? "PUT" : "POST", body }
      )
      toast.success(id ? tr("updated") : tr("created"))
      router.push(`/monitors/view/?id=${res.id}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : tr("error"))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="max-w-3xl">
      <Section title={tr("sections.target")} className="mt-0">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={tr("name")}>
            <Input
              required
              value={m.name}
              onChange={(e) => set("name", e.target.value)}
              placeholder={tr("namePlaceholder")}
            />
          </Field>
          <Field label={tr("type")} hint={tr(`types.${t.value}.hint`)}>
            <Select
              items={typeItems}
              value={m.type}
              onValueChange={(v) => v && setType(v as MonitorType)}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {typeItems.map((x) => (
                  <SelectItem key={x.value} value={x.value}>
                    {x.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {isPush ? (
            <p className="text-xs text-muted-foreground sm:col-span-2">
              {tr("pushHint")}
            </p>
          ) : (
            <Field
              label={
                isHTTP
                  ? tr("target.url")
                  : m.type === "tcp"
                    ? tr("target.hostPort")
                    : tr(`target.${t.target ?? "host"}`)
              }
              className="sm:col-span-2"
            >
              <Input
                required
                value={m.target}
                onChange={(e) => set("target", e.target.value)}
                placeholder={t.placeholder}
              />
            </Field>
          )}
          {m.type === "docker" && (
            <p className="text-xs text-muted-foreground sm:col-span-2">
              {tr("dockerHint")}
            </p>
          )}
          {m.type === "grpc" && (
            <p className="text-xs text-muted-foreground sm:col-span-2">
              {tr("grpcHint")}
            </p>
          )}
          {isSQL && (
            <>
              <Field
                label={tr("query")}
                hint={tr("queryHint")}
                className="sm:col-span-2"
              >
                <Textarea
                  rows={2}
                  value={m.body}
                  onChange={(e) => set("body", e.target.value)}
                  placeholder="SELECT 1"
                />
              </Field>
              <Field
                label={tr("expectedValue")}
                hint={tr("expectedValueHint")}
                className="sm:col-span-2"
              >
                <Input
                  value={m.keyword}
                  onChange={(e) => set("keyword", e.target.value)}
                  placeholder="1"
                />
              </Field>
            </>
          )}
          {hasTLS && (
            <label className="flex items-center gap-2 text-xs text-muted-foreground sm:col-span-2">
              <Switch
                checked={m.ignore_tls}
                onCheckedChange={(v) => set("ignore_tls", v)}
              />
              {tr("ignoreTls")}
            </label>
          )}
          {m.type === "keyword" && (
            <>
              <Field
                label={tr("keyword")}
                hint={tr("keywordHint")}
              >
                <Input
                  required
                  value={m.keyword}
                  onChange={(e) => set("keyword", e.target.value)}
                  placeholder="ok"
                />
              </Field>
              <Field label={tr("invert")}>
                <label className="flex h-8 items-center gap-2 text-xs text-muted-foreground">
                  <Switch
                    checked={m.keyword_invert}
                    onCheckedChange={(v) => set("keyword_invert", v)}
                  />
                  {tr("invertHint")}
                </label>
              </Field>
            </>
          )}
          {m.type === "dns" && (
            <>
              <Field label={tr("record")}>
                <Select
                  items={records.map((r) => ({ value: r, label: r }))}
                  value={m.dns_record || "A"}
                  onValueChange={(v) => v && set("dns_record", v)}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {records.map((r) => (
                      <SelectItem key={r} value={r}>
                        {r}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label={tr("resolver")} hint={tr("resolverHint")}>
                <Input
                  value={m.dns_server}
                  onChange={(e) => set("dns_server", e.target.value)}
                  placeholder="1.1.1.1"
                />
              </Field>
              <Field
                label={tr("expectedAnswer")}
                hint={tr("expectedAnswerHint")}
                className="sm:col-span-2"
              >
                <Input
                  value={m.dns_expected}
                  onChange={(e) => set("dns_expected", e.target.value)}
                  placeholder="93.184.216.34"
                />
              </Field>
            </>
          )}
        </div>
      </Section>

      <Section title={tr("sections.labels")}>
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
          <Field
            label={tr("group")}
            hint={tr("groupHint")}
          >
            <Select
              items={groupItems}
              value={String(m.group_id ?? 0)}
              onValueChange={(v) => set("group_id", Number(v ?? 0))}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {groupItems.map((g) => (
                  <SelectItem key={g.value} value={g.value}>
                    {g.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field
            label={tr("tags")}
            hint={tr("tagsHint")}
          >
            <TagPicker
              value={m.tag_ids ?? []}
              onChange={(ids) => set("tag_ids", ids)}
              tags={labels.tags.data ?? []}
              onCreated={() => labels.tags.mutate()}
            />
          </Field>
        </div>
      </Section>

      <Section title={tr("sections.schedule")}>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field
            label={isPush ? tr("expectedEvery") : tr("interval")}
            hint={isPush ? tr("expectedEveryHint") : undefined}
          >
            <Select
              items={ivList}
              value={String(m.interval_sec)}
              onValueChange={(v) => v && set("interval_sec", Number(v))}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ivList.map((i) => (
                  <SelectItem key={i.value} value={i.value}>
                    {i.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {isPush && (
            <Field
              label={tr("grace")}
              hint={tr("graceHint")}
            >
              <Select
                items={graceList}
                value={String(m.grace_sec)}
                onValueChange={(v) => v !== null && set("grace_sec", Number(v))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {graceList.map((i) => (
                    <SelectItem key={i.value} value={i.value}>
                      {i.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          {!isPush && (
            <>
              <Field label={tr("timeout")}>
                <Input
                  type="number"
                  min={500}
                  max={60000}
                  step={500}
                  value={m.timeout_ms}
                  onChange={(e) => set("timeout_ms", Number(e.target.value))}
                />
              </Field>
              <Field
                label={tr("retries")}
                hint={tr("retriesHint")}
              >
                <Input
                  type="number"
                  min={1}
                  max={10}
                  value={m.retries}
                  onChange={(e) => set("retries", Number(e.target.value))}
                />
              </Field>
            </>
          )}
        </div>
      </Section>

      {!isPush && (
        <Section title={tr("sections.locations")}>
          <p className="mb-3 text-xs text-muted-foreground">
            {tr("locationsHint")}
          </p>
          <div className="grid gap-2 sm:grid-cols-2">
            {(locations.data ?? []).map((l) => (
              <label
                key={l.id}
                className="flex cursor-pointer items-center gap-2 border border-dashed px-3 py-2 hover:bg-accent/50"
              >
                <Checkbox
                  checked={m.locations.includes(l.id)}
                  onCheckedChange={(v) => toggleLoc(l.id, v)}
                />
                <span className="flex-1 truncate">{l.name}</span>
                <span
                  className={cn(
                    "text-xs",
                    l.online ? "text-up" : "text-muted-foreground"
                  )}
                >
                  {l.kind === "local"
                    ? tr("builtIn")
                    : l.online
                      ? tr("online")
                      : tr("offline")}
                </span>
              </label>
            ))}
          </div>
        </Section>
      )}

      {isHTTP && (
        <Section title="http">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={tr("method")}>
              <Select
                items={methods.map((x) => ({ value: x, label: x }))}
                value={m.method || "GET"}
                onValueChange={(v) => v && set("method", v)}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {methods.map((x) => (
                    <SelectItem key={x} value={x}>
                      {x}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field
              label={tr("acceptedStatus")}
              hint={tr("acceptedStatusHint")}
            >
              <Input
                value={m.expected_status}
                onChange={(e) => set("expected_status", e.target.value)}
                placeholder="200-399"
              />
            </Field>
            <Field
              label={tr("headers")}
              hint={tr("headersHint")}
              className="sm:col-span-2"
            >
              <Textarea
                rows={3}
                value={headers}
                onChange={(e) => setHeaders(e.target.value)}
                placeholder="Authorization: Bearer ..."
              />
            </Field>
            {!["GET", "HEAD"].includes(m.method) && (
              <Field label={tr("body")} className="sm:col-span-2">
                <Textarea
                  rows={3}
                  value={m.body}
                  onChange={(e) => set("body", e.target.value)}
                />
              </Field>
            )}
            <label className="flex items-center gap-2 text-xs text-muted-foreground sm:col-span-2">
              <Switch
                checked={m.ignore_tls}
                onCheckedChange={(v) => set("ignore_tls", v)}
              />
              {tr("ignoreTls")}
            </label>
          </div>
        </Section>
      )}

      {!isPush && (
        <Section title={tr("sections.expiry")}>
          <p className="mb-3 text-xs text-muted-foreground">
            {tr("expiryHint")}
          </p>
          <div className="grid gap-2">
            <label
              className={cn(
                "flex items-center gap-2 text-xs",
                !isHTTPS && "text-muted-foreground"
              )}
            >
              <Switch
                checked={m.cert_expiry_alert}
                onCheckedChange={(v) => set("cert_expiry_alert", v)}
              />
              {tr("certExpiry")}
              {!isHTTPS && <span>{tr("certExpiryOnlyHttps")}</span>}
            </label>
            <label className="flex items-center gap-2 text-xs">
              <Switch
                checked={m.domain_expiry_alert}
                onCheckedChange={(v) => set("domain_expiry_alert", v)}
              />
              {tr("domainExpiry")}
              <span className="text-muted-foreground">
                {tr("domainExpiryHint")}
              </span>
            </label>
          </div>
        </Section>
      )}

      <div className="mt-8 flex gap-2 border-t border-dashed pt-4">
        <Button type="submit" disabled={busy}>
          {busy ? tr("saving") : id ? tr("save") : tr("create")}
        </Button>
        <Button type="button" variant="ghost" onClick={() => router.back()}>
          {tc("cancel")}
        </Button>
      </div>
    </form>
  )
}
