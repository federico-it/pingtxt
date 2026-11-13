"use client"

import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Button } from "@/components/ui/button"
import { api, useApi, type TokenScope } from "@/lib/api"
import { cn } from "@/lib/utils"

// An app signed in with OAuth: an AI assistant on the MCP server, or the iOS app.
type OAuthGrant = {
  id: number
  client_id: string
  client_name: string
  scope: TokenScope
  created_at: number
  last_used_at: number
  /** Phone that gets push notifications through this sign-in. */
  push_device?: string
}

/** Client id of the iOS app (internal/hub/mcpauth.go). */
const IOS_CLIENT = "pingtxt-ios"

function date(ms: number) {
  return new Date(ms).toLocaleDateString()
}

/**
 * Apps signed in with OAuth, with a button to disconnect each one.
 * "assistants" lists the MCP clients, "ios" the sign-ins of the iOS app.
 */
export function ConnectedApps({ kind, empty }: { kind: "assistants" | "ios"; empty?: React.ReactNode }) {
  const { data, mutate } = useApi<OAuthGrant[]>("/api/oauth/grants", {
    refreshInterval: 0,
  })
  const tt = useTranslations("apiTokens")

  async function disconnect(g: OAuthGrant) {
    try {
      await api(`/api/oauth/grants/${g.id}`, { method: "DELETE" })
      toast.success(tt("disconnected", { name: g.client_name }))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : tt("error"))
    }
  }

  if (!data) return null
  const list = data.filter((g) => (g.client_id === IOS_CLIENT) === (kind === "ios"))
  if (list.length === 0) return empty ? <>{empty}</> : null

  return (
    <div className="divide-y divide-dashed border border-dashed">
      {list.map((g) => (
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
              {g.push_device && <> · {tt("pushDevice", { name: g.push_device })}</>}
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
  )
}
