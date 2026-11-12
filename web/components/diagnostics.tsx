"use client"

import { useState } from "react"
import {
  AlertTriangleIcon,
  CheckIcon,
  ChevronRightIcon,
  CircleCheckIcon,
  CircleIcon,
  CircleXIcon,
  CopyIcon,
  type LucideIcon,
} from "lucide-react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { segment } from "@/components/ui/control"
import { useApi, type Diag, type DiagFinding, type Diagnostic } from "@/lib/api"
import { datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

type Level = "down" | "warn" | "ok" | "none"

/** Diagnostics of an incident, loaded on demand. */
export function IncidentDiagnostics({ incident }: { incident: number }) {
  const { data, error } = useApi<Diagnostic[]>(`/api/incidents/${incident}/diagnostics`)
  const t = useTranslations("diagnostics")
  if (error) return <p className="text-xs text-down">{t("loadError")}</p>
  if (!data) return <p className="text-xs text-muted-foreground">{t("loading")}</p>
  return <DiagnosticsView list={data} />
}

/**
 * One tab per location. Each shows the diagnosis in plain words, then the
 * steps (DNS, connection, TLS, HTTP, route) with their raw data folded away.
 */
export function DiagnosticsView({ list }: { list: Diagnostic[] }) {
  const [sel, setSel] = useState(0)
  const t = useTranslations("diagnostics")
  const tf = useFinding()
  if (list.length === 0) return <p className="text-xs text-muted-foreground">{t("none")}</p>
  const cur = list[Math.min(sel, list.length - 1)]
  return (
    <div className="grid gap-3 text-xs">
      <div className="flex flex-wrap items-center gap-1">
        {list.length > 1 &&
          list.map((d, i) => (
            <button key={d.id} onClick={() => setSel(i)} className={segment({ active: d === cur })}>
              <Dot level={topLevel(d.findings)} />
              {d.location_name}
            </button>
          ))}
        {list.length === 1 && <span className="font-medium">{cur.location_name}</span>}
        <span className="ml-auto text-muted-foreground">
          {datetime(cur.at)} · {t("took", { ms: Math.round(cur.data.ms) })}
        </span>
        <CopyButton text={diagText(cur, tf)} />
      </div>
      <Verdict findings={cur.findings ?? []} check={cur.data.check} />
      <Steps d={cur.data} findings={cur.findings ?? []} />
    </div>
  )
}

/** Translates a finding, falling back to its English text for codes this UI does not know. */
function useFinding() {
  const t = useTranslations("diagnostics.find")
  return (f: DiagFinding) => {
    const code = f.code as Parameters<typeof t>[0]
    if (!t.has(code)) return f.text
    const p = f.params ?? {}
    const values: Record<string, string> = {}
    for (const k of PARAMS) values[k] = p[k] ?? ""
    return t(code, values)
  }
}

const PARAMS = [
  "host", "error", "port", "family", "failed", "total", "addr", "days", "date", "sans", "issuer",
  "banner", "status", "server", "retry", "hop", "where", "query", "resolver", "answers",
] as const

function topLevel(findings?: DiagFinding[]): Level {
  return (findings?.[0]?.level as Level) ?? "none"
}

function Dot({ level }: { level: Level }) {
  return (
    <span
      className={cn(
        "mr-1.5 inline-block size-1.5 rounded-full",
        level === "down" ? "bg-down" : level === "warn" ? "bg-warn" : level === "ok" ? "bg-up" : "bg-muted-foreground"
      )}
    />
  )
}

/** The diagnosis: the main finding, the other problems and the check's own error. */
function Verdict({ findings, check }: { findings: DiagFinding[]; check?: string }) {
  const t = useTranslations("diagnostics")
  const tf = useFinding()
  const main = findings[0]
  const more = findings.slice(1).filter((f) => f.level !== "ok").slice(0, 3)
  const level = (main?.level as Level) ?? "none"
  const Icon: LucideIcon = level === "down" ? CircleXIcon : level === "warn" ? AlertTriangleIcon : CircleCheckIcon
  return (
    <div
      className={cn(
        "grid gap-1.5 border-l-2 bg-background p-3",
        level === "down" ? "border-down" : level === "warn" ? "border-warn" : "border-up"
      )}
    >
      <div className="text-[11px] text-muted-foreground uppercase">{t("diagnosis")}</div>
      {main ? (
        <div className="flex gap-2 text-[13px] leading-snug">
          <Icon className={cn("mt-0.5 size-4 shrink-0", level === "down" ? "text-down" : level === "warn" ? "text-warn" : "text-up")} />
          <span>
            {tf(main)}
            {main.params?.retry && <span className="text-muted-foreground"> {t("retryAfter", { retry: main.params.retry })}</span>}
          </span>
        </div>
      ) : (
        <p className="text-muted-foreground">{t("noFindings")}</p>
      )}
      {more.map((f) => (
        <div key={f.code} className="flex gap-2 pl-6">
          <span className={f.level === "down" ? "text-down" : "text-warn"}>•</span>
          <span>{tf(f)}</span>
        </div>
      ))}
      {check && (
        <div className="pl-6 text-muted-foreground">
          {t("checkError")}: <span className="break-all text-foreground">{check}</span>
        </div>
      )}
    </div>
  )
}

/** One line per step, with its raw data behind a toggle. */
function Steps({ d, findings }: { d: Diag; findings: DiagFinding[] }) {
  const t = useTranslations("diagnostics")
  const levelOf = (step: string, fallback: Level): Level => {
    const f = findings.find((x) => x.step === step && x.level !== "ok")
    return (f?.level as Level) ?? fallback
  }
  const steps: { key: string; label: string; level: Level; line: React.ReactNode; details?: React.ReactNode }[] = []

  if (d.dns?.length) {
    const q = d.dns.find((x) => x.resolver === "system" && x.query.startsWith("A/AAAA ")) ?? d.dns[0]
    const failed = d.dns.some((x) => x.error)
    steps.push({
      key: "dns",
      label: t("steps.dns"),
      level: levelOf("dns", failed ? "down" : "ok"),
      line: (
        <>
          {q.query.replace(/^A\/AAAA /, "")} → {q.error ? humanErr(t, q.error) : q.answers?.length ? plusMore(q.answers) : t("noRecords")}
          <Muted> · {fmtMs(q.ms)}</Muted>
        </>
      ),
      details: d.dns.map((x, i) => (
        <Row key={i} k={x.resolver === "system" ? t("systemResolver") : x.resolver} tone={x.error ? "down" : undefined}>
          <Muted>{x.query} → </Muted>
          {x.error || (x.answers?.length ? x.answers.join(", ") : t("noRecords"))}
          {x.cname && <Muted> (CNAME {x.cname})</Muted>}
          <Muted> · {fmtMs(x.ms)}</Muted>
        </Row>
      )),
    })
  }

  if (d.tcp?.length) {
    const ok = d.tcp.filter((c) => c.ok)
    const bad = d.tcp.filter((c) => !c.ok)
    steps.push({
      key: "tcp",
      label: t("steps.tcp", { port: d.port ?? "" }),
      level: levelOf("tcp", bad.length === 0 ? "ok" : ok.length === 0 ? "down" : "warn"),
      line:
        ok.length > 0 ? (
          <>
            {t("portOpen", { ok: ok.length, total: d.tcp.length })}
            <Muted> · {fmtMs(ok[0].ms)}</Muted>
            {d.banner && <Muted> · {d.banner}</Muted>}
          </>
        ) : (
          <>{humanErr(t, bad[0].error ?? "")}</>
        ),
      details: (
        <>
          {d.tcp.map((c) => (
            <Row key={c.addr} k={c.addr} tone={c.ok ? undefined : "down"}>
              {c.ok ? t("open") : c.error}
              <Muted> · {fmtMs(c.ms)}</Muted>
            </Row>
          ))}
          {d.banner && <Row k={t("banner")}>{d.banner}</Row>}
        </>
      ),
    })
  }

  if (d.tls) {
    const x = d.tls
    steps.push({
      key: "tls",
      label: t("steps.tls"),
      level: levelOf("tls", x.error ? "down" : "ok"),
      line: x.error ? (
        <>{x.error}</>
      ) : (
        <>
          {[x.version, x.not_after ? t("validUntil", { date: datetime(x.not_after * 1000) }) : ""].filter(Boolean).join(" · ")}
          {x.issuer && <Muted> · {x.issuer}</Muted>}
        </>
      ),
      details: (
        <>
          {x.subject && <Row k={t("subject")}>{x.subject}</Row>}
          {x.issuer && <Row k={t("issuer")}>{x.issuer}</Row>}
          {x.not_after ? <Row k={t("until")}>{datetime(x.not_after * 1000)}</Row> : null}
          {x.sans?.length ? <Row k="SAN">{x.sans.join(", ")}</Row> : null}
          <Row k={t("address")}>{x.addr}</Row>
        </>
      ),
    })
  }

  if (d.http) {
    const x = d.http
    const server = x.headers?.find((h) => h.name.toLowerCase() === "server")?.value
    steps.push({
      key: "http",
      label: t("steps.http"),
      level: levelOf("http", x.status >= 400 ? "down" : "ok"),
      line: (
        <>
          {x.status}
          {server && <Muted> · {server}</Muted>}
          {x.redirects?.length ? <Muted> · {t("redirects", { n: x.redirects.length })}</Muted> : null}
        </>
      ),
      details: (
        <>
          {x.proto && <Row k={t("protocol")}>{x.proto}</Row>}
          {x.remote_addr && <Row k={t("address")}>{x.remote_addr}</Row>}
          {x.redirects?.map((r) => (
            <Row key={r} k="↪">{r}</Row>
          ))}
          {x.headers?.length ? (
            <>
              <div className="mt-2 mb-1 text-[11px] text-muted-foreground uppercase">{t("headers")}</div>
              {x.headers.map((h, i) => (
                <Row key={`${h.name}${i}`} k={h.name.toLowerCase()}>
                  {h.value === "[redacted]" ? <span className="text-muted-foreground italic">{t("redacted")}</span> : h.value}
                </Row>
              ))}
            </>
          ) : null}
        </>
      ),
    })
  }

  if (d.trace) {
    const x = d.trace
    const last = lastAnswered(x.hops)
    steps.push({
      key: "trace",
      label: t("steps.trace"),
      level: x.error ? "none" : levelOf("trace", x.reached ? "ok" : "warn"),
      line: x.error ? (
        <Muted>{x.error}</Muted>
      ) : x.reached ? (
        <>{t("reachedIn", { n: x.hops?.length ?? 0 })}</>
      ) : last ? (
        <>{t("stopsAt", { hop: last.ttl, where: last.name || last.addr || "" })}</>
      ) : (
        <>{t("noHop")}</>
      ),
      details: x.error ? undefined : <TraceTable trace={x} />,
    })
  }

  return (
    <div className="grid">
      {steps.map(({ key, ...s }) => (
        <Step key={key} {...s} />
      ))}
      {d.note && (
        <div className="border-t border-dashed py-2 text-muted-foreground">
          {t("note")}: {d.note}
        </div>
      )}
    </div>
  )
}

const stepIcon: Record<Level, { icon: LucideIcon; cls: string }> = {
  down: { icon: CircleXIcon, cls: "text-down" },
  warn: { icon: AlertTriangleIcon, cls: "text-warn" },
  ok: { icon: CircleCheckIcon, cls: "text-up" },
  none: { icon: CircleIcon, cls: "text-muted-foreground" },
}

function Step({ label, level, line, details }: { label: string; level: Level; line: React.ReactNode; details?: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  const t = useTranslations("diagnostics")
  const { icon: Icon, cls } = stepIcon[level]
  return (
    <div className="border-t border-dashed">
      <button
        type="button"
        onClick={() => details && setOpen(!open)}
        disabled={!details}
        aria-expanded={details ? open : undefined}
        className="grid w-full grid-cols-[1rem_6.5rem_1fr_auto] items-start gap-2 py-2 text-left disabled:cursor-default sm:grid-cols-[1rem_9rem_1fr_auto]"
      >
        <Icon className={cn("mt-px size-3.5", cls)} />
        <span className="text-muted-foreground">{label}</span>
        <span className={cn("min-w-0 break-words", level === "down" && "text-down")}>{line}</span>
        {details ? (
          <span className="flex items-center text-muted-foreground">
            <span className="hidden sm:inline">{t("details")}</span>
            <ChevronRightIcon className={cn("size-3.5 transition-transform", open && "rotate-90")} />
          </span>
        ) : (
          <span />
        )}
      </button>
      {open && details && <div className="mb-2 ml-6 border-l pl-3">{details}</div>}
    </div>
  )
}

function TraceTable({ trace }: { trace: NonNullable<Diag["trace"]> }) {
  const last = !trace.reached ? lastAnswered(trace.hops) : undefined
  return (
    <>
      <div className="mb-1 text-[11px] text-muted-foreground uppercase">
        {trace.proto} {trace.dest}
        {trace.port ? `:${trace.port}` : ""}
      </div>
      <table className="w-full">
        <tbody>
          {(trace.hops ?? []).map((h) => (
            <tr key={h.ttl} className={cn("border-t border-dashed", h === last && "text-warn")}>
              <td className="w-8 py-1 pr-3 text-right text-muted-foreground tabular-nums">{h.ttl}</td>
              <td className="py-1 pr-3 break-all">
                {h.addr ? (
                  <>
                    {h.name || h.addr}
                    {h.name && <Muted> {h.addr}</Muted>}
                  </>
                ) : (
                  <Muted>*</Muted>
                )}
              </td>
              <td className="py-1 text-right whitespace-nowrap text-muted-foreground tabular-nums">
                {h.addr && h.rtt_ms !== undefined ? fmtMs(h.rtt_ms) : ""}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}

function Row({ k, children, tone }: { k: string; children: React.ReactNode; tone?: "down" }) {
  return (
    <div className="grid grid-cols-[minmax(6rem,auto)_1fr] gap-3 py-1 sm:grid-cols-[10rem_1fr]">
      <div className="truncate text-muted-foreground" title={k}>
        {k}
      </div>
      <div className={cn("min-w-0 break-all", tone === "down" && "text-down")}>{children}</div>
    </div>
  )
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  const t = useTranslations("diagnostics")
  async function copy() {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard blocked: nothing to do
    }
  }
  return (
    <Button variant="ghost" size="xs" onClick={copy} aria-label={t("copy")}>
      {copied ? <CheckIcon className="text-up" /> : <CopyIcon />}
      {t("copy")}
    </Button>
  )
}

/** Puts the usual network errors in plain words; others are shown as they are. */
function humanErr(t: ReturnType<typeof useTranslations<"diagnostics">>, e: string) {
  const s = e.toLowerCase()
  if (s.includes("refused")) return t("err.refused")
  if (s.includes("timeout") || s.includes("timed out") || s.includes("deadline")) return t("err.timeout")
  if (s.includes("no such host")) return t("err.nxdomain")
  if (s.includes("unreachable") || s.includes("no route")) return t("err.unreachable")
  return e
}

function plusMore(list: string[]) {
  return list.length > 1 ? `${list[0]} +${list.length - 1}` : list[0]
}

function lastAnswered(hops: NonNullable<Diag["trace"]>["hops"]) {
  const list = hops ?? []
  for (let i = list.length - 1; i >= 0; i--) if (list[i].addr) return list[i]
  return undefined
}

function fmtMs(v: number) {
  return v < 10 ? `${v.toFixed(1)} ms` : `${Math.round(v)} ms`
}

/** Plain text of one location's diagnostics, to paste in a ticket. */
function diagText(x: Diagnostic, tf: (f: DiagFinding) => string) {
  const d = x.data
  const out = [`# ${x.location_name} · ${new Date(x.at).toISOString()}`]
  for (const f of x.findings ?? []) out.push(`${f.level === "down" ? "✗" : f.level === "warn" ? "!" : "✓"} ${tf(f)}`)
  if (d.check) out.push(`check  ${d.check}`)
  for (const q of d.dns ?? [])
    out.push(`dns${q.resolver === "system" ? "" : " @" + q.resolver}  ${q.query} -> ${q.error || (q.answers ?? []).join(", ")}${q.cname ? ` (CNAME ${q.cname})` : ""} (${fmtMs(q.ms)})`)
  for (const c of d.tcp ?? []) out.push(`tcp  ${c.addr} ${c.ok ? "open" : c.error} (${fmtMs(c.ms)})`)
  if (d.banner) out.push(`banner  ${d.banner}`)
  if (d.tls)
    out.push(`tls  ${[d.tls.version, d.tls.subject, d.tls.issuer && "issuer " + d.tls.issuer, d.tls.error].filter(Boolean).join(" · ")}`)
  if (d.http) {
    out.push(`http  ${[d.http.proto, d.http.status, d.http.remote_addr].filter(Boolean).join(" ")}`)
    for (const r of d.http.redirects ?? []) out.push(`  -> ${r}`)
    for (const h of d.http.headers ?? []) out.push(`  ${h.name}: ${h.value}`)
  }
  if (d.trace) {
    out.push(`traceroute ${d.trace.proto} ${d.trace.dest}:${d.trace.port}${d.trace.error ? " " + d.trace.error : d.trace.reached ? "" : " (not reached)"}`)
    for (const h of d.trace.hops ?? [])
      out.push(`  ${String(h.ttl).padStart(2)}  ${h.addr ? (h.name ? `${h.name} (${h.addr})` : h.addr) : "*"}${h.addr && h.rtt_ms !== undefined ? "  " + fmtMs(h.rtt_ms) : ""}`)
  }
  if (d.note) out.push(`note  ${d.note}`)
  return out.join("\n")
}
