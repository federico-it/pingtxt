"use client"

import { useState } from "react"
import { toast } from "sonner"

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

const scopes: { value: TokenScope; label: string }[] = [
  { value: "read", label: "read only" },
  { value: "write", label: "read + write" },
]

const expiries = [
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
  { value: "0", label: "never" },
]

function date(ms: number) {
  return new Date(ms).toLocaleDateString()
}

export function ApiTokens() {
  const { data, mutate } = useApi<ApiToken[]>("/api/tokens", {
    refreshInterval: 0,
  })
  const [name, setName] = useState("")
  const [scope, setScope] = useState<TokenScope>("read")
  // A token never has more rights than its owner.
  const isAdmin = useIsAdmin()
  const allowed = isAdmin ? scopes : scopes.filter((s) => s.value === "read")
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
      toast.error(err instanceof Error ? err.message : "error")
    } finally {
      setBusy(false)
    }
  }

  async function revoke(t: ApiToken) {
    try {
      await api(`/api/tokens/${t.id}`, { method: "DELETE" })
      toast.success(`${t.name} revoked`)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  const origin = typeof window === "undefined" ? "" : window.location.origin
  const now = useNow()

  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted-foreground">
        tokens let scripts and tools use the API. they cannot change accounts or
        settings.
      </p>

      {created ? (
        <div className="grid gap-3">
          <p className="text-xs">
            copy the token now, it will not be shown again.
          </p>
          <CopyBlock code={created} />
          <CopyBlock
            code={`curl -H "Authorization: Bearer ${created}" ${origin}/api/monitors`}
          />
          <div>
            <Button variant="outline" onClick={() => setCreated(null)}>
              done
            </Button>
          </div>
        </div>
      ) : (
        <form onSubmit={create} className="grid gap-3 sm:grid-cols-3">
          <Field label="name" htmlFor="token-name">
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
            label="access"
            hint={
              !isAdmin
                ? "your account is read-only: tokens can only read"
                : scope === "read"
                  ? "view monitors, incidents, agents"
                  : "also create, edit and delete"
            }
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
          <Field label="expires">
            <Select
              items={expiries}
              value={expiry}
              onValueChange={(v) => v && setExpiry(v)}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {expiries.map((x) => (
                  <SelectItem key={x.value} value={x.value}>
                    {x.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <div className="sm:col-span-3">
            <Button type="submit" variant="outline" disabled={busy}>
              create token
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
                    [{t.scope}]
                  </span>
                  <span className="block text-[11px] text-muted-foreground">
                    ptk_…{t.hint} · created {date(t.created_at)} ·{" "}
                    {t.last_used_at
                      ? `last used ${date(t.last_used_at)}`
                      : "never used"}{" "}
                    ·{" "}
                    {t.expires_at === 0 ? (
                      "no expiry"
                    ) : expired ? (
                      <span className="text-down">
                        expired {date(t.expires_at)}
                      </span>
                    ) : (
                      `expires ${date(t.expires_at)}`
                    )}
                  </span>
                </div>
                <Confirm
                  trigger={
                    <Button size="xs" variant="ghost">
                      revoke
                    </Button>
                  }
                  title={`revoke ${t.name}?`}
                  description="Scripts using this token stop working right away."
                  action="revoke"
                  onConfirm={() => revoke(t)}
                />
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
