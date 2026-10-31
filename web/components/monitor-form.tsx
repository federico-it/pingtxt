"use client"

import { useRouter } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

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

const types: {
  value: MonitorType
  label: string
  hint: string
  placeholder: string
  /** Label of the target field; defaults to "host". */
  target?: string
}[] = [
  {
    value: "http",
    label: "http(s)",
    hint: "request a URL and check the status code",
    placeholder: "https://example.com/health",
  },
  {
    value: "keyword",
    label: "keyword",
    hint: "request a URL and look for a string in the body",
    placeholder: "https://example.com",
  },
  {
    value: "tcp",
    label: "tcp port",
    hint: "open a TCP connection",
    placeholder: "db.example.com:5432",
  },
  {
    value: "ping",
    label: "ping (icmp)",
    hint: "send ICMP echo requests",
    placeholder: "1.1.1.1",
  },
  {
    value: "dns",
    label: "dns",
    hint: "resolve a record and optionally match the answer",
    placeholder: "example.com",
  },
  {
    value: "postgres",
    label: "postgresql",
    hint: "connect and run a query",
    placeholder: "postgres://user:password@db.example.com:5432/app",
    target: "connection url",
  },
  {
    value: "mysql",
    label: "mysql / mariadb",
    hint: "connect and run a query",
    placeholder: "mysql://user:password@db.example.com:3306/app",
    target: "connection url",
  },
  {
    value: "redis",
    label: "redis",
    hint: "authenticate and send PING",
    placeholder: "redis://:password@cache.example.com:6379/0",
    target: "connection url",
  },
  {
    value: "mongodb",
    label: "mongodb",
    hint: "authenticate (SCRAM) and run ping",
    placeholder:
      "mongodb://user:password@db.example.com:27017/?authSource=admin",
    target: "connection url",
  },
  {
    value: "docker",
    label: "docker container",
    hint: "down when the container is not running or unhealthy",
    placeholder: "my-app-web-1",
    target: "container name or id",
  },
  {
    value: "grpc",
    label: "grpc health",
    hint: "call grpc.health.v1.Health/Check",
    placeholder: "grpcs://api.example.com:443/my.Service",
    target: "address",
  },
  {
    value: "smtp",
    label: "smtp",
    hint: "read the greeting, EHLO and STARTTLS when offered",
    placeholder: "smtp://mail.example.com:587",
    target: "server",
  },
  {
    value: "push",
    label: "heartbeat (push)",
    hint: "your cron job or service calls a URL; down when the calls stop",
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
  { value: "0", label: "none" },
  { value: "60", label: "1m" },
  { value: "300", label: "5m" },
  { value: "900", label: "15m" },
  { value: "1800", label: "30m" },
  { value: "3600", label: "1h" },
  { value: "21600", label: "6h" },
]

/** Adds the current value to a preset list so the select can show it. */
function withValue(list: { value: string; label: string }[], sec: number) {
  return list.some((x) => x.value === String(sec))
    ? list
    : [...list, { value: String(sec), label: interval(sec) }].sort(
        (a, b) => Number(a.value) - Number(b.value)
      )
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
  degraded_pct: 0,
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
  const groupItems = [
    { value: "0", label: "no group" },
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
  const graceList = withValue(graces, m.grace_sec)

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
      toast.success(id ? "monitor updated" : "monitor created")
      router.push(`/monitors/view/?id=${res.id}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="max-w-3xl">
      <Section title="target" className="mt-0">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="name">
            <Input
              required
              value={m.name}
              onChange={(e) => set("name", e.target.value)}
              placeholder="api production"
            />
          </Field>
          <Field label="type" hint={t.hint}>
            <Select
              items={types.map((x) => ({ value: x.value, label: x.label }))}
              value={m.type}
              onValueChange={(v) => v && setType(v as MonitorType)}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {types.map((x) => (
                  <SelectItem key={x.value} value={x.value}>
                    {x.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {isPush ? (
            <p className="text-xs text-muted-foreground sm:col-span-2">
              pingtxt gives this monitor a secret URL. Call it at the end of
              every run of your job: the monitor goes down when no call arrives
              within the expected period plus the grace time. The URL is shown
              after saving.
            </p>
          ) : (
            <Field
              label={
                isHTTP
                  ? "url"
                  : m.type === "tcp"
                    ? "host:port"
                    : (t.target ?? "host")
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
              Checked on the docker engine of each selected location: the hub or
              agent needs access to /var/run/docker.sock (or DOCKER_HOST). With
              docker, mount it with -v
              /var/run/docker.sock:/var/run/docker.sock:ro.
            </p>
          )}
          {m.type === "grpc" && (
            <p className="text-xs text-muted-foreground sm:col-span-2">
              grpc:// for plaintext, grpcs:// for TLS. The path is the service
              name to check; leave it out to check the whole server.
            </p>
          )}
          {isSQL && (
            <>
              <Field
                label="query"
                hint="optional, default SELECT 1"
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
                label="expected value"
                hint="optional: first column of the first row must equal this"
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
              ignore TLS certificate errors
            </label>
          )}
          {m.type === "keyword" && (
            <>
              <Field
                label="keyword"
                hint="case-sensitive substring of the response body"
              >
                <Input
                  required
                  value={m.keyword}
                  onChange={(e) => set("keyword", e.target.value)}
                  placeholder="ok"
                />
              </Field>
              <Field label="invert">
                <label className="flex h-8 items-center gap-2 text-xs text-muted-foreground">
                  <Switch
                    checked={m.keyword_invert}
                    onCheckedChange={(v) => set("keyword_invert", v)}
                  />
                  fail when the keyword is present
                </label>
              </Field>
            </>
          )}
          {m.type === "dns" && (
            <>
              <Field label="record">
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
              <Field label="resolver" hint="empty = system resolver">
                <Input
                  value={m.dns_server}
                  onChange={(e) => set("dns_server", e.target.value)}
                  placeholder="1.1.1.1"
                />
              </Field>
              <Field
                label="expected answer"
                hint="optional: fail if no answer contains this"
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

      <Section title="group & tags">
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
          <Field
            label="group"
            hint="groups show as sections on the dashboard and status pages"
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
            label="tags"
            hint="type a name and press enter to create a tag"
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

      <Section title="schedule">
        <div className="grid gap-4 sm:grid-cols-3">
          <Field
            label={isPush ? "expected every" : "interval"}
            hint={isPush ? "how often your job runs" : undefined}
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
              label="grace time"
              hint="extra wait before the monitor goes down, e.g. for slow runs"
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
              <Field label="timeout (ms)">
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
                label="retries"
                hint="consecutive failures before a location is down"
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
        <Section title="locations">
          <p className="mb-3 text-xs text-muted-foreground">
            The monitor is down when more than half of the online locations
            fail, degraded when at least the threshold below fails.
          </p>
          <div className="mb-4 max-w-xs">
            <Field
              label="degraded at (% of locations)"
              hint="empty = instance default (settings)"
            >
              <Input
                type="number"
                min={1}
                max={50}
                placeholder="default"
                value={m.degraded_pct || ""}
                onChange={(e) =>
                  set(
                    "degraded_pct",
                    Math.min(Math.max(Number(e.target.value) || 0, 0), 50)
                  )
                }
              />
            </Field>
          </div>
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
                    ? "built-in"
                    : l.online
                      ? "online"
                      : "offline"}
                </span>
              </label>
            ))}
          </div>
        </Section>
      )}

      {isHTTP && (
        <Section title="http">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="method">
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
              label="accepted status"
              hint="e.g. 200-299,301 or 2xx. empty = 200-399"
            >
              <Input
                value={m.expected_status}
                onChange={(e) => set("expected_status", e.target.value)}
                placeholder="200-399"
              />
            </Field>
            <Field
              label="headers"
              hint="one per line: Name: value"
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
              <Field label="body" className="sm:col-span-2">
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
              ignore TLS certificate errors
            </label>
          </div>
        </Section>
      )}

      {!isPush && (
        <Section title="expiry alerts">
          <p className="mb-3 text-xs text-muted-foreground">
            Notify before the TLS certificate or the domain registration
            expires. Thresholds are set in settings.
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
              tls certificate
              {!isHTTPS && <span>(only for https urls)</span>}
            </label>
            <label className="flex items-center gap-2 text-xs">
              <Switch
                checked={m.domain_expiry_alert}
                onCheckedChange={(v) => set("domain_expiry_alert", v)}
              />
              domain registration
              <span className="text-muted-foreground">
                (rdap / whois, not for ip addresses)
              </span>
            </label>
          </div>
        </Section>
      )}

      <div className="mt-8 flex gap-2 border-t border-dashed pt-4">
        <Button type="submit" disabled={busy}>
          {busy ? "saving..." : id ? "save" : "create monitor"}
        </Button>
        <Button type="button" variant="ghost" onClick={() => router.back()}>
          cancel
        </Button>
      </div>
    </form>
  )
}
