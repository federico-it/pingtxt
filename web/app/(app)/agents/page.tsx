"use client"

import { useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"

import { AgentInstall } from "@/components/agent-install"
import { Confirm } from "@/components/confirm"
import { Empty, PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api, useApi, useIsAdmin, type Agent } from "@/lib/api"
import { ago } from "@/lib/format"

type AgentsResp = { agents: Agent[]; hub_url: string; hub_version: string; auto_update: boolean }

export default function AgentsPage() {
  const isAdmin = useIsAdmin()
  const { data, mutate } = useApi<AgentsResp>("/api/agents", {
    // Poll while an update runs so the new version shows up on its own.
    refreshInterval: (d) => (d?.agents.some((a) => a.update_state === "updating") ? 2000 : 15000),
  })
  const [edit, setEdit] = useState<{ id?: number; name: string; location: string } | null>(null)
  const [install, setInstall] = useState<{ name: string; token: string; hub: string } | null>(null)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    if (!edit) return
    try {
      if (edit.id) {
        await api(`/api/agents/${edit.id}`, { method: "PUT", body: edit })
        toast.success("agent updated")
      } else {
        const r = await api<{ agent: Agent; token: string; hub_url: string }>("/api/agents", { method: "POST", body: edit })
        setInstall({ name: r.agent.name, token: r.token, hub: r.hub_url })
      }
      setEdit(null)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  async function rotate(a: Agent) {
    const r = await api<{ token: string; hub_url: string }>(`/api/agents/${a.id}/token`, { method: "POST" })
    setInstall({ name: a.name, token: r.token, hub: r.hub_url })
    mutate()
  }

  async function setAutoUpdate(on: boolean) {
    try {
      await api("/api/settings/agent-updates", { method: "PUT", body: { auto_update: on } })
      toast.success(on ? "automatic updates on" : "automatic updates off")
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  async function updateNow(a: Agent) {
    try {
      await api(`/api/agents/${a.id}/update`, { method: "POST" })
      toast.success(`updating ${a.name} to ${data?.hub_version}`)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  async function remove(a: Agent) {
    await api(`/api/agents/${a.id}`, { method: "DELETE" })
    toast.success("agent deleted")
    mutate()
  }

  return (
    <>
      <PageHeader cmd="pingtxt agents ls" title="agents">
        {isAdmin && (
          <Button onClick={() => setEdit({ name: "", location: "" })}>
            <PlusIcon /> add agent
          </Button>
        )}
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        Agents are tiny probes you run anywhere (VPS, home lab, another cloud region) to check your monitors from more
        than one place. The hub itself is always available as the built-in location.
      </p>

      {data && (
        <div className="mb-6 flex max-w-2xl flex-wrap items-start gap-x-6 gap-y-2 border border-border px-3 py-2 text-xs">
          <label className="flex items-center gap-2">
            <Switch checked={data.auto_update} onCheckedChange={setAutoUpdate} disabled={!isAdmin} />
            <span>update agents automatically</span>
          </label>
          <span className="text-muted-foreground">
            hub <span className="text-foreground">{data.hub_version}</span>
            {" · "}
            {data.hub_version === "dev"
              ? "development build: agents are not updated"
              : "agents installed with the script update to this version when they connect. Docker agents update with the image."}
          </span>
        </div>
      )}

      {data && data.agents.length === 0 ? (
        <Empty>no agents yet. add one to monitor from a second location.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>state</TableHead>
              <TableHead>name</TableHead>
              <TableHead>location</TableHead>
              <TableHead className="hidden md:table-cell">host</TableHead>
              <TableHead className="hidden md:table-cell">version</TableHead>
              <TableHead className="text-right">seen</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(data?.agents ?? []).map((a) => (
              <TableRow key={a.id}>
                <TableCell>{a.online ? <span className="text-up">● online</span> : <span className="text-muted-foreground">○ offline</span>}</TableCell>
                <TableCell className="font-medium">{a.name}</TableCell>
                <TableCell>{a.location || <span className="text-muted-foreground">--</span>}</TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">
                  {a.hostname ? `${a.hostname} · ${a.platform} · ${a.remote_addr}` : "never connected"}
                </TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">
                  <VersionCell agent={a} hubVersion={data?.hub_version ?? ""} />
                </TableCell>
                <TableCell className="text-right text-muted-foreground">{a.online ? "now" : ago(a.last_seen)}</TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  {isAdmin && (
                    <>
                      {a.online && a.update_available && a.self_update && a.update_state !== "updating" && (
                        <Button variant="ghost" size="xs" onClick={() => updateNow(a)}>
                          update
                        </Button>
                      )}
                      <Button variant="ghost" size="xs" onClick={() => setEdit({ id: a.id, name: a.name, location: a.location })}>
                        edit
                      </Button>
                      <Confirm
                        title={`rotate token of ${a.name}?`}
                        description="The current token stops working immediately and the agent disconnects. You will get new install commands."
                        action="rotate"
                        onConfirm={() => rotate(a)}
                        trigger={
                          <Button variant="ghost" size="xs">
                            token
                          </Button>
                        }
                      />
                      <Confirm
                        title={`delete ${a.name}?`}
                        description="The agent is disconnected and removed from all monitors. Past results are kept."
                        onConfirm={() => remove(a)}
                        trigger={
                          <Button variant="ghost" size="xs" className="text-down">
                            rm
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
              <DialogTitle>{edit?.id ? "edit agent" : "add agent"}</DialogTitle>
              <DialogDescription>The location label is what you see on charts, e.g. &quot;Frankfurt&quot;.</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5">
              <Label htmlFor="agent-name">name</Label>
              <Input id="agent-name" required value={edit?.name ?? ""} onChange={(e) => setEdit((p) => p && { ...p, name: e.target.value })} placeholder="vps-fra-1" />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="agent-loc">location</Label>
              <Input id="agent-loc" value={edit?.location ?? ""} onChange={(e) => setEdit((p) => p && { ...p, location: e.target.value })} placeholder="Frankfurt" />
            </div>
            <DialogFooter>
              <Button type="submit">{edit?.id ? "save" : "create"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!install} onOpenChange={(o) => !o && setInstall(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>install {install?.name}</DialogTitle>
            <DialogDescription>Run one of these on the machine that should become a monitoring location.</DialogDescription>
          </DialogHeader>
          {install && <AgentInstall hub={install.hub} token={install.token} />}
          <DialogFooter showCloseButton />
        </DialogContent>
      </Dialog>
    </>
  )
}

function VersionCell({ agent: a, hubVersion }: { agent: Agent; hubVersion: string }) {
  if (!a.version) return <>--</>
  if (a.update_state === "updating") {
    return (
      <span>
        {a.version} <span className="text-warn">→ {hubVersion} updating…</span>
      </span>
    )
  }
  if (a.update_state === "failed") {
    return (
      <Tooltip>
        <TooltipTrigger render={<span className="cursor-help" />}>
          {a.version} <span className="text-down">update failed</span>
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
      <TooltipContent>{hint ?? (a.online ? "a new version is available" : "updates when the agent reconnects")}</TooltipContent>
    </Tooltip>
  )
}
