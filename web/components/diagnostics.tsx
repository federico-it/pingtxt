"use client"

import { useState } from "react"
import { CheckIcon, CopyIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { segment } from "@/components/ui/control"
import { useApi, type Diag, type Diagnostic } from "@/lib/api"
import { datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

/** Diagnostics of an incident, loaded on demand. */
export function IncidentDiagnostics({ incident }: { incident: number }) {
  const { data, error } = useApi<Diagnostic[]>(`/api/incidents/${incident}/diagnostics`)
  const t = useTranslations("diagnostics")
  if (error) return <p className="text-xs text-down">{t("loadError")}</p>
  if (!data) return <p className="text-xs text-muted-foreground">{t("loading")}</p>
  return <DiagnosticsView list={data} />
}

/** One tab per location; each shows DNS, TCP, TLS, HTTP and the traceroute. */
export function DiagnosticsView({ list }: { list: Diagnostic[] }) {
  const [sel, setSel] = useState(0)
  const t = useTranslations("diagnostics")
  if (list.length === 0) return <p className="text-xs text-muted-foreground">{t("none")}</p>
  const cur = list[Math.min(sel, list.length - 1)]
  return (
    <div className="grid gap-3 text-xs">
      <div className="flex flex-wrap items-center gap-1">
        {list.map((d, i) => (
          <button key={d.id} onClick={() => setSel(i)} className={segment({ active: d === cur })}>
            {d.location_name}
          </button>
        ))}
        <span className="ml-auto text-muted-foreground">
          {datetime(cur.at)} · {t("took", { ms: Math.round(cur.data.ms) })}
        </span>
        <CopyButton text={diagText(cur)} />
      </div>
      <DiagBody d={cur.data} />
    </div>
  )
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

function Row({ k, children, tone }: { k: string; children: React.ReactNode; tone?: "down" | "warn" }) {
  return (
    <div className="grid grid-cols-[7rem_1fr] gap-3 border-t border-dashed py-1.5 sm:grid-cols-[9rem_1fr]">
      <div className="text-muted-foreground">{k}</div>
      <div className={cn("min-w-0 break-all", tone === "down" && "text-down", tone === "warn" && "text-warn")}>{children}</div>
    </div>
  )
}

function DiagBody({ d }: { d: Diag }) {
  const t = useTranslations("diagnostics")
  return (
    <div className="grid gap-4">
      <div>
        {(d.dns ?? []).map((q, i) => (
          <Row key={`dns${i}`} k={q.resolver === "system" ? "dns" : `dns @${q.resolver}`} tone={q.error ? "down" : undefined}>
            <span className="text-muted-foreground">{q.query} → </span>
            {q.error || (q.answers?.length ? q.answers.join(", ") : t("noRecords"))}
            {q.cname && <span className="text-muted-foreground"> (CNAME {q.cname})</span>}
            <span className="text-muted-foreground"> · {fmtMs(q.ms)}</span>
          </Row>
        ))}
        {(d.tcp ?? []).map((c) => (
          <Row key={c.addr} k="tcp" tone={c.ok ? undefined : "down"}>
            {c.addr} {c.ok ? t("open") : c.error}
            <span className="text-muted-foreground"> · {fmtMs(c.ms)}</span>
          </Row>
        ))}
        {d.tls && (
          <Row k="tls" tone={d.tls.error ? "down" : undefined}>
            {[d.tls.version, d.tls.subject].filter(Boolean).join(" ")}
            {d.tls.issuer && <span className="text-muted-foreground"> · {t("issuer")} {d.tls.issuer}</span>}
            {d.tls.not_after ? (
              <span className="text-muted-foreground"> · {t("until")} {datetime(d.tls.not_after * 1000)}</span>
            ) : null}
            {d.tls.error && <div>{d.tls.error}</div>}
            {d.tls.sans?.length ? <div className="text-muted-foreground">SAN: {d.tls.sans.join(", ")}</div> : null}
          </Row>
        )}
        {d.http && (
          <Row k="http">
            {[d.http.proto, d.http.status].filter(Boolean).join(" ")}
            {d.http.remote_addr && <span className="text-muted-foreground"> · {d.http.remote_addr}</span>}
            {d.http.redirects?.map((r) => (
              <div key={r} className="text-muted-foreground">↪ {r}</div>
            ))}
          </Row>
        )}
        {d.note && <Row k={t("note")}>{d.note}</Row>}
      </div>

      {d.http?.headers?.length ? (
        <div>
          <div className="mb-1 text-[11px] text-muted-foreground uppercase">{t("headers")}</div>
          {d.http.headers.map((h, i) => (
            <Row key={`${h.name}${i}`} k={h.name.toLowerCase()}>
              <span className={h.value === "[redacted]" ? "text-muted-foreground italic" : undefined}>
                {h.value === "[redacted]" ? t("redacted") : h.value}
              </span>
            </Row>
          ))}
        </div>
      ) : null}

      {d.trace && (
        <div>
          <div className="mb-1 flex flex-wrap gap-x-2 text-[11px] text-muted-foreground uppercase">
            <span>
              traceroute · {d.trace.proto} {d.trace.dest}:{d.trace.port}
            </span>
            {!d.trace.error && (
              <span className={d.trace.reached ? "text-up" : "text-warn"}>
                {d.trace.reached ? t("reached") : t("notReached")}
              </span>
            )}
          </div>
          {d.trace.error ? (
            <p className="text-muted-foreground">{d.trace.error}</p>
          ) : (
            <table className="w-full">
              <tbody>
                {(d.trace.hops ?? []).map((h) => {
                  const last = !d.trace!.reached && h === lastAnswered(d.trace!.hops)
                  return (
                    <tr key={h.ttl} className={cn("border-t border-dashed", last && "text-warn")}>
                      <td className="w-8 py-1 pr-3 text-right text-muted-foreground tabular-nums">{h.ttl}</td>
                      <td className="py-1 pr-3 break-all">
                        {h.addr ? (
                          <>
                            {h.name || h.addr}
                            {h.name && <span className="text-muted-foreground"> {h.addr}</span>}
                          </>
                        ) : (
                          <span className="text-muted-foreground">*</span>
                        )}
                      </td>
                      <td className="py-1 text-right text-muted-foreground tabular-nums whitespace-nowrap">
                        {h.addr && h.rtt_ms !== undefined ? fmtMs(h.rtt_ms) : ""}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
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
function diagText(x: Diagnostic) {
  const d = x.data
  const out = [`# ${x.location_name} · ${new Date(x.at).toISOString()}`]
  for (const q of d.dns ?? [])
    out.push(`dns${q.resolver === "system" ? "" : " @" + q.resolver}  ${q.query} -> ${q.error || (q.answers ?? []).join(", ")}${q.cname ? ` (CNAME ${q.cname})` : ""} (${fmtMs(q.ms)})`)
  for (const c of d.tcp ?? []) out.push(`tcp  ${c.addr} ${c.ok ? "open" : c.error} (${fmtMs(c.ms)})`)
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
