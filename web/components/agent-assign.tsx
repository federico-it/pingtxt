"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { api, useApi, type Agent, type AgentMonitorPlan } from "@/lib/api"
import { cn } from "@/lib/utils"

/**
 * Asks to add an agent to every compatible monitor. Monitors that cannot use
 * it (IPv6 monitor on an agent without IPv6, Docker, localhost targets) are
 * listed apart with the reason.
 */
export function AgentAssign({
  agent,
  onClose,
  onDone,
}: {
  agent: Agent | null
  onClose: () => void
  onDone?: () => void
}) {
  const t = useTranslations("agentAssign")
  // The ipv6 state is part of the key: the plan changes once the agent reports it.
  const { data: plan } = useApi<AgentMonitorPlan>(agent ? `/api/agents/${agent.id}/monitors?ipv6=${agent.ipv6}` : null, {
    refreshInterval: 0,
    revalidateOnFocus: false,
  })
  // Everything compatible starts selected: keep the ones unticked. The parent
  // remounts this component (key) for each agent.
  const [unpicked, setUnpicked] = useState<Set<number>>(new Set())
  const [saving, setSaving] = useState(false)
  const compatible = plan?.compatible ?? []
  const skipped = plan?.skipped ?? []
  const picked = new Set(compatible.filter((m) => !unpicked.has(m.id)).map((m) => m.id))

  function toggle(id: number, on: boolean) {
    setUnpicked((p) => {
      const n = new Set(p)
      if (on) n.delete(id)
      else n.add(id)
      return n
    })
  }

  async function assign() {
    if (!agent) return
    setSaving(true)
    try {
      const r = await api<{ added: number; skipped: unknown[] }>(`/api/agents/${agent.id}/monitors`, {
        method: "POST",
        body: { monitor_ids: [...picked] },
      })
      toast.success(t("added", { count: r.added, name: agent.name }))
      onDone?.()
      onClose()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setSaving(false)
    }
  }

  const all = compatible.length > 0 && picked.size === compatible.length

  return (
    <Dialog open={!!agent} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("title", { name: agent?.name ?? "" })}</DialogTitle>
          <DialogDescription>
            {agent?.online ? t("textOnline", { name: agent.name }) : t("text", { name: agent?.name ?? "" })}
          </DialogDescription>
        </DialogHeader>

        {!plan ? (
          <p className="text-xs text-muted-foreground">{t("loading")}</p>
        ) : (
          <div className="grid min-w-0 gap-4 text-sm">
            {compatible.length === 0 ? (
              <p className="text-xs text-muted-foreground">{plan.already > 0 ? t("noneLeft", { count: plan.already }) : t("none")}</p>
            ) : (
              <div className="grid gap-1.5">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                  <span>{t("compatible", { count: compatible.length })}</span>
                  <button
                    type="button"
                    className="hover:text-foreground"
                    onClick={() => setUnpicked(all ? new Set(compatible.map((m) => m.id)) : new Set())}
                  >
                    {all ? t("selectNone") : t("selectAll")}
                  </button>
                </div>
                <div className="grid max-h-64 gap-1 overflow-y-auto">
                  {compatible.map((m) => (
                    <label key={m.id} className="flex min-w-0 cursor-pointer items-center gap-2 border border-dashed px-3 py-1.5 hover:bg-accent/50">
                      <Checkbox checked={picked.has(m.id)} onCheckedChange={(v) => toggle(m.id, v)} />
                      <span className="truncate">{m.name}</span>
                      <span className="ml-auto shrink-0 text-xs text-muted-foreground">{m.type}</span>
                    </label>
                  ))}
                </div>
              </div>
            )}

            {skipped.length > 0 && (
              <div className="grid gap-1.5">
                <span className="text-xs text-muted-foreground">{t("skipped", { count: skipped.length })}</span>
                <ul className="grid max-h-40 gap-1 overflow-y-auto text-xs">
                  {skipped.map((m) => (
                    <li key={m.id} className="flex min-w-0 items-baseline gap-2 px-3 py-1 opacity-70">
                      <span className="shrink-0 text-muted-foreground">--</span>
                      <span className="min-w-0 flex-1 truncate">{m.name}</span>
                      <span className={cn("max-w-[60%] shrink-0 text-right", m.reason === "ipv6_unknown" ? "text-muted-foreground" : "text-warn")}>
                        {t(`reasons.${m.reason ?? "docker"}`)}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {plan.already > 0 && compatible.length > 0 && (
              <p className="text-xs text-muted-foreground">{t("already", { count: plan.already })}</p>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {t("later")}
          </Button>
          {compatible.length > 0 && (
            <Button onClick={assign} disabled={saving || picked.size === 0}>
              {t("add", { count: picked.size })}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
