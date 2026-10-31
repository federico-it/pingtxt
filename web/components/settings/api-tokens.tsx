"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { CopyBlock } from "@/components/copy-block"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useNow } from "@/hooks/use-now"
import {
  api,
  useApi,
  useIsAdmin,
  type ApiToken,
  type TokenScope,
} from "@/lib/api"
import { cn } from "@/lib/utils"

// Labels are in apiTokens.scopes.<scope> and apiTokens.expiries.<days>.
const scopes: TokenScope[] = ["read", "write"]
const expiries = ["30", "90", "365", "0"] as const

// An AI assistant connected through OAuth to the MCP server.
type OAuthGrant = {
  id: number
  client_name: string
  scope: TokenScope
  created_at: number
  last_used_at: number
}

function date(ms: number) {
  return new Date(ms).toLocaleDateString()
}

export function ApiTokens() {
  const { data, mutate } = useApi<ApiToken[]>("/api/tokens", {
    refreshInterval: 0,
  })
  const { data: grants, mutate: mutateGrants } = useApi<OAuthGrant[]>(
    "/api/oauth/grants",
    { refreshInterval: 0 }
  )
  const [name, setName] = useState("")
  const [scope, setScope] = useState<TokenScope>("read")
  // A token never has more rights than its owner.
  const isAdmin = useIsAdmin()
  const tt = useTranslations("apiTokens")
  const allowed = (isAdmin ? scopes : scopes.filter((s) => s === "read")).map((value) => ({ value, label: tt(`scopes.${value}`) }))
  const expiryItems = expiries.map((value) => ({ value, label: tt(`expiries.${value}`) }))
  const [expiry, setExpiry] = useState("90")
  const [created, setCreated] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function create(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api<{ secret: string }>("/api/tokens", {
        method: "POST",
        body: { name, scope, expires_in_days: Number(expiry) },
      })
      setCreated(r.secret)
      setName("")
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : tt("error"))
    } finally {
      setBusy(false)
    }
  }

  async function revoke(t: ApiToken) {
    try {
      await api(`/api/tokens/${t.id}`, { method: "DELETE" })
      toast.success(tt("revoked", { name: t.name }))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : tt("error"))
    }
  }

  async function disconnect(g: OAuthGrant) {
    try {
      await api(`/api/oauth/grants/${g.id}`, { method: "DELETE" })
      toast.success(tt("disconnected", { name: g.client_name }))
      mutateGrants()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : tt("error"))
    }
  }

  const origin = typeof window === "undefined" ? "" : window.location.origin
  const now = useNow()

  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted-foreground">
        {tt("intro")}{" "}
        {tt.rich("mcpIntro", {
          url: origin + "/mcp",
          b: (c) => <span className="text-foreground">{c}</span>,
        })}
      </p>

      {created ? (
        <div className="grid gap-3">
          <p className="text-xs">
            {tt("copyNow")}
          </p>
          <CopyBlock code={created} />
          <CopyBlock
            code={`curl -H "Authorization: Bearer ${created}" ${origin}/api/monitors`}
          />
          <p className="text-xs text-muted-foreground">{tt("mcpClaudeCode")}</p>
          <CopyBlock
            code={`claude mcp add --transport http pingtxt ${origin}/mcp --header "Authorization: Bearer ${created}"`}
          />
          <div>
            <Button variant="outline" onClick={() => setCreated(null)}>
              {tt("done")}
            </Button>
          </div>
        </div>
      ) : (
        <form onSubmit={create} className="grid gap-3 sm:grid-cols-3">
          <Field label={tt("name")} htmlFor="token-name">
            <Input
              id="token-name"
              required
              maxLength={100}
              placeholder="grafana"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field
            label={tt("access")}
            hint={!isAdmin ? tt("viewerHint") : scope === "read" ? tt("readHint") : tt("writeHint")}
          >
            <Select
              items={allowed}
              value={scope}
              onValueChange={(v) => v && setScope(v as TokenScope)}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {allowed.map((s) => (
                  <SelectItem key={s.value} value={s.value}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label={tt("expires")}>
            <Select
              items={expiryItems}
              value={expiry}
              onValueChange={(v) => v && setExpiry(v)}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {expiryItems.map((x) => (
                  <SelectItem key={x.value} value={x.value}>
                    {x.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <div className="sm:col-span-3">
            <Button type="submit" variant="outline" disabled={busy}>
              {tt("create")}
            </Button>
          </div>
        </form>
      )}

      {data && data.length > 0 && (
        <div className="divide-y divide-dashed border border-dashed">
          {data.map((t) => {
            const expired = t.expires_at > 0 && t.expires_at <= now
            return (
              <div
                key={t.id}
                className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-xs"
              >
                <div className="min-w-0">
                  <span className="text-foreground">{t.name}</span>{" "}
                  <span
                    className={cn(
                      "text-muted-foreground",
                      t.scope === "write" && "text-warn"
                    )}
                  >
                    [{t.scope === "write" || t.scope === "read" ? tt(`scopes.${t.scope}`) : t.scope}]
                  </span>
                  <span className="block text-[11px] text-muted-foreground">
                    ptk_…{t.hint} · {tt("created", { date: date(t.created_at) })} ·{" "}
                    {t.last_used_at ? tt("lastUsed", { date: date(t.last_used_at) }) : tt("neverUsed")} ·{" "}
                    {t.expires_at === 0 ? (
                      tt("noExpiry")
                    ) : expired ? (
                      <span className="text-down">{tt("expired", { date: date(t.expires_at) })}</span>
                    ) : (
                      tt("expiresOn", { date: date(t.expires_at) })
                    )}
                  </span>
                </div>
                <Confirm
                  trigger={
                    <Button size="xs" variant="ghost">
                      {tt("revoke")}
                    </Button>
                  }
                  title={tt("revokeTitle", { name: t.name })}
                  description={tt("revokeText")}
                  action={tt("revoke")}
                  onConfirm={() => revoke(t)}
                />
              </div>
            )
          })}
        </div>
      )}
      {grants && grants.length > 0 && (
        <div className="grid gap-2">
          <p className="text-xs text-muted-foreground">{tt("connectedApps")}</p>
          <div className="divide-y divide-dashed border border-dashed">
            {grants.map((g) => (
              <div
                key={g.id}
                className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-xs"
              >
                <div className="min-w-0">
                  <span className="text-foreground">{g.client_name}</span>{" "}
                  <span
                    className={cn(
                      "text-muted-foreground",
                      g.scope === "write" && "text-warn"
                    )}
                  >
                    [{tt(`scopes.${g.scope}`)}]
                  </span>
                  <span className="block text-[11px] text-muted-foreground">
                    {tt("connectedOn", { date: date(g.created_at) })} ·{" "}
                    {g.last_used_at
                      ? tt("lastUsed", { date: date(g.last_used_at) })
                      : tt("neverUsed")}
                  </span>
                </div>
                <Confirm
                  trigger={
                    <Button size="xs" variant="ghost">
                      {tt("disconnect")}
                    </Button>
                  }
                  title={tt("disconnectTitle", { name: g.client_name })}
                  description={tt("disconnectText")}
                  action={tt("disconnect")}
                  onConfirm={() => disconnect(g)}
                />
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
