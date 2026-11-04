"use client"

import { useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { AgentAssign } from "@/components/agent-assign"
import { AgentInstall } from "@/components/agent-install"
import { Confirm } from "@/components/confirm"
import { Empty, PageHeader } from "@/components/page-header"
import { CountryFlag, CountrySelect } from "@/components/country"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api, useApi, useIsAdmin, type Agent, type LocalAgent } from "@/lib/api"
import { segment } from "@/components/ui/control"
import { useMinute } from "@/hooks/use-now"
import { countryName } from "@/lib/country"
import { ago, datetime } from "@/lib/format"
import { useLocale } from "@/lib/i18n"
import { cn } from "@/lib/utils"

type AgentsResp = {
  agents: Agent[]
  /** the hub's own location (older hubs leave it out) */
  local?: LocalAgent
  hub_url: string
  hub_version: string
  auto_update: boolean
  offline_grace_min: number
}

const maxGraceMin = 1440

// Agents already asked about (per browser), so the prompt shows once.
const ASKED_KEY = "pingtxt.agents.assignAsked"

function loadAsked(): number[] {
  try {
    return JSON.parse(localStorage.getItem(ASKED_KEY) ?? "[]")
  } catch {
    return []
  }
}

function saveAsked(ids: number[]) {
  try {
    localStorage.setItem(ASKED_KEY, JSON.stringify(ids.slice(-100)))
  } catch {
    // private mode: the prompt may show again
  }
}

export default function AgentsPage() {
  const isAdmin = useIsAdmin()
  const t = useTranslations("agents")
  const { locale } = useLocale()
  const [edit, setEdit] = useState<{
    id?: number
    /** the hub's own location: name, country and switch only */
    local?: boolean
    name: string
    location: string
    country: string
    alert_offline: boolean
    enabled?: boolean
  } | null>(null)
  const [onlyOffline, setOnlyOffline] = useState(false)
  const [grace, setGrace] = useState<string | null>(null)
  const now = useMinute()
  const [install, setInstall] = useState<{ id: number; name: string; token: string; hub: string; isNew: boolean } | null>(null)
  const [assignId, setAssignId] = useState<number | null>(null)
  const { data, mutate } = useApi<AgentsResp>("/api/agents", {
    // Poll fast while an update runs or a new agent is expected, so the change
    // shows up on its own.
    refreshInterval: (d) =>
      d?.agents.some((a) => a.update_state === "updating") || install?.isNew || (assignId && d?.agents.find((a) => a.id === assignId)?.ipv6 === 0)
        ? 2000
        : 15000,
    // A new agent that comes online and checks no monitor yet: ask once
    // whether to add it to the compatible monitors.
    onSuccess: (d) => {
      if (!isAdmin || assignId) return
      const asked = loadAsked()
      const fresh = d.agents.find((a) => a.online && a.monitors === 0 && !asked.includes(a.id))
      if (!fresh) return
      saveAsked([...asked, fresh.id])
      // The install commands are done once their agent connected.
      setInstall((i) => (i && i.id === fresh.id ? null : i))
      setAssignId(fresh.id)
    },
  })
  const assignAgent = data?.agents.find((a) => a.id === assignId) ?? null

  async function save(e: React.FormEvent) {
    e.preventDefault()
    if (!edit) return
    try {
      if (edit.local) {
        await api("/api/agents/local", { method: "PUT", body: { name: edit.name, country: edit.country, enabled: edit.enabled } })
        toast.success(t("updated"))
      } else if (edit.id) {
        await api(`/api/agents/${edit.id}`, { method: "PUT", body: edit })
        toast.success(t("updated"))
      } else {
        const r = await api<{ agent: Agent; token: string; hub_url: string }>("/api/agents", { method: "POST", body: edit })
        setInstall({ id: r.agent.id, name: r.agent.name, token: r.token, hub: r.hub_url, isNew: true })
      }
      setEdit(null)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function rotate(a: Agent) {
    const r = await api<{ token: string; hub_url: string }>(`/api/agents/${a.id}/token`, { method: "POST" })
    setInstall({ id: a.id, name: a.name, token: r.token, hub: r.hub_url, isNew: false })
    mutate()
  }

  async function setAutoUpdate(on: boolean) {
    try {
      await api("/api/settings/agent-updates", { method: "PUT", body: { auto_update: on } })
      toast.success(on ? t("autoUpdateOn") : t("autoUpdateOff"))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function saveGrace() {
    if (grace === null || !data) return
    const min = Math.round(Number(grace))
    setGrace(null)
    if (!Number.isFinite(min) || min === data.offline_grace_min) return
    if (min < 1 || min > maxGraceMin) {
      toast.error(t("graceRange", { max: maxGraceMin }))
      return
    }
    try {
      await api("/api/settings/agent-updates", { method: "PUT", body: { offline_grace_min: min } })
      toast.success(t("graceSaved", { min }))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  const graceMs = (data?.offline_grace_min ?? 5) * 60_000
  // Offline longer than the grace period: alerted (unless muted), checks not running.
  const lost = (a: Agent) => !a.online && a.offline_since > 0 && now - a.offline_since >= graceMs
  const offlineCount = data?.agents.filter((a) => !a.online).length ?? 0
  const shown = (data?.agents ?? []).filter((a) => !onlyOffline || !a.online)

  async function updateNow(a: Agent) {
    try {
      await api(`/api/agents/${a.id}/update`, { method: "POST" })
      toast.success(t("updating", { name: a.name, version: data?.hub_version ?? "" }))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function remove(a: Agent) {
    await api(`/api/agents/${a.id}`, { method: "DELETE" })
    toast.success(t("deleted"))
    mutate()
  }

  return (
    <>
      <PageHeader cmd="pingtxt agents ls" title={t("title")}>
        {isAdmin && (
          <Button onClick={() => setEdit({ name: "", location: "", country: "", alert_offline: true })}>
            <PlusIcon /> {t("add")}
          </Button>
        )}
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        {t("intro")}
      </p>

      {data && (
        <div className="mb-6 flex max-w-2xl flex-wrap items-start gap-x-6 gap-y-2 border border-border px-3 py-2 text-xs">
          <label className="flex items-center gap-2">
            <Switch checked={data.auto_update} onCheckedChange={setAutoUpdate} disabled={!isAdmin} />
            <span>{t("autoUpdate")}</span>
          </label>
          <label className="flex items-center gap-2" title={t("graceHint")}>
            <span>{t("graceBefore")}</span>
            <Input
              type="number"
              min={1}
              max={maxGraceMin}
              className="h-6 w-16 px-1.5 text-xs"
              value={grace ?? String(data.offline_grace_min)}
              disabled={!isAdmin}
              onChange={(e) => setGrace(e.target.value)}
              onBlur={saveGrace}
              onKeyDown={(e) => e.key === "Enter" && (e.currentTarget as HTMLInputElement).blur()}
            />
            <span>{t("graceAfter")}</span>
          </label>
          <span className="text-muted-foreground">
            {t.rich("hubVersion", { version: data.hub_version, hl: (c) => <span className="text-foreground">{c}</span> })}
            {" · "}
            {data.hub_version === "dev" ? t("devBuild") : t("updateHint")}
          </span>
        </div>
      )}

      {offlineCount > 0 && (
        <div className="mb-4 flex flex-wrap gap-1 text-xs">
          <button onClick={() => setOnlyOffline(false)} className={segment({ active: !onlyOffline })}>
            {t("filterAll")}:{data?.agents.length ?? 0}
          </button>
          <button onClick={() => setOnlyOffline(true)} className={segment({ active: onlyOffline })}>
            {t("offline")}:{offlineCount}
          </button>
        </div>
      )}

      {data && data.agents.length === 0 && !data.local ? (
        <Empty>{t("empty")}</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("cols.state")}</TableHead>
              <TableHead>{t("cols.name")}</TableHead>
              <TableHead>{t("cols.location")}</TableHead>
              <TableHead className="hidden md:table-cell">{t("cols.host")}</TableHead>
              <TableHead className="hidden md:table-cell">{t("cols.version")}</TableHead>
              <TableHead className="text-right">{t("cols.seen")}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.local && !onlyOffline && (
              <TableRow>
                <TableCell>
                  {data.local.enabled ? (
                    <span className="text-up">● {t("online")}</span>
                  ) : (
                    <span className="text-muted-foreground">○ {t("localOff")}</span>
                  )}
                </TableCell>
                <TableCell>
                  <div className="font-medium">
                    {data.local.name}
                    <span className="ml-2 text-[11px] font-normal text-muted-foreground" title={t("builtInHint")}>
                      {t("builtIn")}
                    </span>
                  </div>
                  <div className="text-xs text-muted-foreground">{t("monitors", { count: data.local.monitors })}</div>
                </TableCell>
                <TableCell>
                  <CountryFlag code={data.local.country} />
                  {data.local.country ? countryName(data.local.country, locale) : <span className="text-muted-foreground">--</span>}
                </TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">
                  {t("thisHub")}
                  {data.local.ipv6 > 0 && (
                    <span className={data.local.ipv6 === 1 ? "text-up" : undefined}> · {t(data.local.ipv6 === 1 ? "ipv6Yes" : "ipv6No")}</span>
                  )}
                </TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">{data.local.version}</TableCell>
                <TableCell className="text-right text-muted-foreground">{data.local.enabled ? t("now") : "--"}</TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  {isAdmin && (
                    <Button
                      variant="ghost"
                      size="xs"
                      onClick={() =>
                        setEdit({ local: true, name: data.local!.name, location: "", country: data.local!.country, alert_offline: true, enabled: data.local!.enabled })
                      }
                    >
                      {t("edit")}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            )}
            {shown.map((a) => (
              <TableRow key={a.id}>
                <TableCell>
                  {a.online ? (
                    <span className="text-up">● {t("online")}</span>
                  ) : lost(a) ? (
                    <span className="text-down" title={t("lostHint")}>● {t("offline")}</span>
                  ) : (
                    <span className="text-muted-foreground">○ {t("offline")}</span>
                  )}
                </TableCell>
                <TableCell>
                  <div className="font-medium">
                    {a.name}
                    {!a.alert_offline && (
                      <span className="ml-2 text-[11px] font-normal text-muted-foreground" title={t("alertOffHint")}>
                        {t("alertOff")}
                      </span>
                    )}
                  </div>
                  <MonitorsLine agent={a} onAssign={isAdmin ? () => setAssignId(a.id) : undefined} />
                </TableCell>
                <TableCell>
                  <CountryFlag code={a.country} />
                  {a.location || (a.country ? null : <span className="text-muted-foreground">--</span>)}
                </TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">
                  {a.hostname ? `${a.hostname} · ${a.platform} · ${a.remote_addr}` : t("neverConnected")}
                  {a.ipv6 > 0 && (
                    <span className={a.ipv6 === 1 ? "text-up" : undefined}> · {t(a.ipv6 === 1 ? "ipv6Yes" : "ipv6No")}</span>
                  )}
                </TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">
                  <VersionCell agent={a} hubVersion={data?.hub_version ?? ""} />
                </TableCell>
                <TableCell className={cn("text-right text-muted-foreground", lost(a) && "text-down")} title={a.last_seen ? datetime(a.last_seen) : undefined}>{a.online ? t("now") : ago(a.offline_since || a.last_seen)}</TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  {isAdmin && (
                    <>
                      {a.online && a.update_available && a.self_update && a.update_state !== "updating" && (
                        <Button variant="ghost" size="xs" onClick={() => updateNow(a)}>
                          {t("update")}
                        </Button>
                      )}
                      <Button variant="ghost" size="xs" onClick={() => setEdit({ id: a.id, name: a.name, location: a.location, country: a.country ?? "", alert_offline: a.alert_offline })}>
                        {t("edit")}
                      </Button>
                      <Confirm
                        title={t("rotateTitle", { name: a.name })}
                        description={t("rotateText")}
                        action={t("rotate")}
                        onConfirm={() => rotate(a)}
                        trigger={
                          <Button variant="ghost" size="xs">
                            {t("token")}
                          </Button>
                        }
                      />
                      <Confirm
                        title={t("deleteTitle", { name: a.name })}
                        description={t("deleteText")}
                        onConfirm={() => remove(a)}
                        trigger={
                          <Button variant="ghost" size="xs" className="text-down">
                            {t("rm")}
                          </Button>
                        }
                      />
                    </>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Dialog open={!!edit} onOpenChange={(o) => !o && setEdit(null)}>
        <DialogContent>
          <form onSubmit={save} className="grid gap-4">
            <DialogHeader>
              <DialogTitle>{edit?.local ? t("editLocalTitle") : edit?.id ? t("editTitle") : t("add")}</DialogTitle>
              <DialogDescription>{edit?.local ? t("localDialogText") : t("dialogText")}</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5">
              <Label htmlFor="agent-name">{t("name")}</Label>
              <Input id="agent-name" required value={edit?.name ?? ""} onChange={(e) => setEdit((p) => p && { ...p, name: e.target.value })} placeholder="vps-fra-1" />
            </div>
            {!edit?.local && (
              <div className="grid gap-1.5">
                <Label htmlFor="agent-loc">{t("location")}</Label>
                <Input id="agent-loc" value={edit?.location ?? ""} onChange={(e) => setEdit((p) => p && { ...p, location: e.target.value })} placeholder="Frankfurt" />
              </div>
            )}
            <div className="grid gap-1.5">
              <Label htmlFor="agent-country">{t("country")}</Label>
              <CountrySelect id="agent-country" value={edit?.country ?? ""} onChange={(v) => setEdit((p) => p && { ...p, country: v })} />
              <span className="text-xs text-muted-foreground">{t("countryHint")}</span>
            </div>
            {edit?.local && (
              <label className="flex cursor-pointer items-start gap-2">
                <Switch className="mt-0.5" checked={!!edit.enabled} onCheckedChange={(v) => setEdit((p) => p && { ...p, enabled: v })} />
                <span className="grid gap-0.5">
                  <span>{t("localEnabled")}</span>
                  <span className="text-xs text-muted-foreground">{t("localEnabledHint")}</span>
                </span>
              </label>
            )}
            {edit?.id && (
              <label className="flex cursor-pointer items-start gap-2">
                <Switch className="mt-0.5" checked={edit.alert_offline} onCheckedChange={(v) => setEdit((p) => p && { ...p, alert_offline: v })} />
                <span className="grid gap-0.5">
                  <span>{t("alertOffline")}</span>
                  <span className="text-xs text-muted-foreground">{t("alertOfflineHint", { min: data?.offline_grace_min ?? 5 })}</span>
                </span>
              </label>
            )}
            <DialogFooter>
              <Button type="submit">{edit?.id || edit?.local ? t("save") : t("create")}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!install} onOpenChange={(o) => !o && setInstall(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t("installTitle", { name: install?.name ?? "" })}</DialogTitle>
            <DialogDescription>{t("installText")}</DialogDescription>
          </DialogHeader>
          {install && <AgentInstall hub={install.hub} token={install.token} />}
          {install?.isNew && (
            <p className="text-xs text-muted-foreground">
              <span className="animate-pulse">●</span> {t("waiting", { name: install.name })}
            </p>
          )}
          <DialogFooter showCloseButton />
        </DialogContent>
      </Dialog>
      <AgentAssign key={assignId ?? 0} agent={assignAgent} onClose={() => setAssignId(null)} onDone={() => mutate()} />
    </>
  )
}

function MonitorsLine({ agent: a, onAssign }: { agent: Agent; onAssign?: () => void }) {
  const t = useTranslations("agents")
  return (
    <div className="text-xs text-muted-foreground">
      {a.monitors === 0 ? <span className="text-warn">{t("noMonitors")}</span> : t("monitors", { count: a.monitors })}
      {onAssign && (
        <>
          {" · "}
          <button type="button" className="underline-offset-2 hover:text-foreground hover:underline" onClick={onAssign}>
            {t("assign")}
          </button>
        </>
      )}
    </div>
  )
}

function VersionCell({ agent: a, hubVersion }: { agent: Agent; hubVersion: string }) {
  const t = useTranslations("agents")
  if (!a.version) return <>--</>
  if (a.update_state === "updating") {
    return (
      <span>
        {a.version} <span className="text-warn">→ {hubVersion} {t("updatingState")}</span>
      </span>
    )
  }
  if (a.update_state === "failed") {
    return (
      <Tooltip>
        <TooltipTrigger render={<span className="cursor-help" />}>
          {a.version} <span className="text-down">{t("updateFailed")}</span>
        </TooltipTrigger>
        <TooltipContent>{a.update_error}</TooltipContent>
      </Tooltip>
    )
  }
  if (!a.update_available) return <>{a.version}</>
  const hint = a.online && !a.self_update ? a.no_update_reason : undefined
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="cursor-help" />}>
        {a.version} <span className="text-warn">→ {hubVersion}</span>
      </TooltipTrigger>
      <TooltipContent>{hint ?? (a.online ? t("newVersion") : t("updatesOnReconnect"))}</TooltipContent>
    </Tooltip>
  )
}
