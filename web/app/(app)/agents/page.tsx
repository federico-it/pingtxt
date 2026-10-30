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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { api, useApi, type Agent } from "@/lib/api"
import { ago } from "@/lib/format"

type AgentsResp = { agents: Agent[]; hub_url: string }

export default function AgentsPage() {
  const { data, mutate } = useApi<AgentsResp>("/api/agents")
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

  async function remove(a: Agent) {
    await api(`/api/agents/${a.id}`, { method: "DELETE" })
    toast.success("agent deleted")
    mutate()
  }

  return (
    <>
      <PageHeader cmd="pingtxt agents ls" title="agents">
        <Button onClick={() => setEdit({ name: "", location: "" })}>
          <PlusIcon /> add agent
        </Button>
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        Agents are tiny probes you run anywhere (VPS, home lab, another cloud region) to check your monitors from more
        than one place. The hub itself is always available as the built-in location.
      </p>

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
                <TableCell className="hidden text-muted-foreground md:table-cell">{a.version || "--"}</TableCell>
                <TableCell className="text-right text-muted-foreground">{a.online ? "now" : ago(a.last_seen)}</TableCell>
                <TableCell className="text-right whitespace-nowrap">
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
