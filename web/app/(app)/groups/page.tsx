"use client"

import { useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { TagChip, useLabels } from "@/components/labels"
import { Empty, PageHeader, Section } from "@/components/page-header"
import { StatusGlyph } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { api, tagColors, useApi, useIsAdmin, type Group, type Monitor, type Tag, type TagColor } from "@/lib/api"
import { cn } from "@/lib/utils"

type GroupDraft = { id?: number; name: string; description: string; monitor_ids: number[] }
type TagDraft = { id?: number; name: string; color: TagColor }

export default function GroupsPage() {
  const { groups, tags } = useLabels()
  const isAdmin = useIsAdmin()
  const t = useTranslations("groups")
  const monitors = useApi<Monitor[]>("/api/monitors", { refreshInterval: 30_000 })
  const [group, setGroup] = useState<GroupDraft | null>(null)
  const [tag, setTag] = useState<TagDraft | null>(null)

  const all = monitors.data ?? []

  function refresh() {
    groups.mutate()
    tags.mutate()
    monitors.mutate()
  }

  function editGroup(g?: Group) {
    setGroup(
      g
        ? { id: g.id, name: g.name, description: g.description, monitor_ids: all.filter((m) => m.group_id === g.id).map((m) => m.id) }
        : { name: "", description: "", monitor_ids: [] }
    )
  }

  async function saveGroup(e: React.FormEvent) {
    e.preventDefault()
    if (!group) return
    try {
      const saved = await api<Group>(group.id ? `/api/groups/${group.id}` : "/api/groups", {
        method: group.id ? "PUT" : "POST",
        body: { name: group.name, description: group.description },
      })
      await api(`/api/groups/${saved.id}/monitors`, { method: "PUT", body: { monitor_ids: group.monitor_ids } })
      toast.success(t("groupSaved"))
      setGroup(null)
      refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function saveTag(e: React.FormEvent) {
    e.preventDefault()
    if (!tag) return
    try {
      await api(tag.id ? `/api/tags/${tag.id}` : "/api/tags", { method: tag.id ? "PUT" : "POST", body: { name: tag.name, color: tag.color } })
      toast.success(t("tagSaved"))
      setTag(null)
      refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function remove(path: string) {
    try {
      await api(path, { method: "DELETE" })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
    refresh()
  }

  const groupName = new Map((groups.data ?? []).map((g) => [g.id, g.name]))

  return (
    <>
      <PageHeader cmd="pingtxt groups ls" title={t("title")}>
        {isAdmin && (
          <>
            <Button variant="outline" onClick={() => setTag({ name: "", color: "" })}>
              <PlusIcon /> {t("tag")}
            </Button>
            <Button onClick={() => editGroup()}>
              <PlusIcon /> {t("group")}
            </Button>
          </>
        )}
      </PageHeader>
      <p className="-mt-3 mb-2 max-w-2xl text-xs text-muted-foreground">
        {t("intro")}
      </p>

      <Section title={t("groupsTitle", { count: groups.data?.length ?? 0 })}>
        {groups.data && groups.data.length === 0 ? (
          <Empty>{t("noGroups")}</Empty>
        ) : (
          <div className="border-t border-dashed">
            {(groups.data ?? []).map((g) => {
              const members = all.filter((m) => m.group_id === g.id)
              return (
                <div key={g.id} className="flex flex-wrap items-center gap-3 border-b border-dashed px-2 py-2.5">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-baseline gap-x-3">
                      <span className="font-medium">[{g.name}]</span>
                      <span className="text-xs text-muted-foreground">
                        {t("monitorCount", { count: g.monitors })}
                      </span>
                    </div>
                    {g.description && <div className="truncate text-xs text-muted-foreground">{g.description}</div>}
                    {members.length > 0 && (
                      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                        {members.map((m) => (
                          <span key={m.id} className="inline-flex items-center gap-1">
                            <StatusGlyph status={m.status} />
                            {m.name}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                  {isAdmin && (
                    <>
                      <Button variant="ghost" size="xs" onClick={() => editGroup(g)}>
                        {t("edit")}
                      </Button>
                      <Confirm
                        title={t("deleteGroup", { name: g.name })}
                        description={t("deleteGroupText")}
                        onConfirm={() => remove(`/api/groups/${g.id}`)}
                        trigger={
                          <Button variant="ghost" size="xs" className="text-down">
                            {t("rm")}
                          </Button>
                        }
                      />
                    </>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </Section>

      <Section title={t("tagsTitle", { count: tags.data?.length ?? 0 })}>
        {tags.data && tags.data.length === 0 ? (
          <Empty>{t("noTags")}</Empty>
        ) : (
          <div className="border-t border-dashed">
            {(tags.data ?? []).map((tg) => (
              <div key={tg.id} className="flex items-center gap-3 border-b border-dashed px-2 py-2">
                <TagChip tag={tg} className="h-5 px-1.5 text-xs" />
                <span className="flex-1 text-xs text-muted-foreground">
                  {t("monitorCount", { count: tg.monitors })}
                </span>
                {isAdmin && (
                  <>
                    <Button variant="ghost" size="xs" onClick={() => setTag({ id: tg.id, name: tg.name, color: tg.color })}>
                      {t("edit")}
                    </Button>
                    <Confirm
                      title={t("deleteTag", { name: tg.name })}
                      description={t("deleteTagText")}
                      onConfirm={() => remove(`/api/tags/${tg.id}`)}
                      trigger={
                        <Button variant="ghost" size="xs" className="text-down">
                          {t("rm")}
                        </Button>
                      }
                    />
                  </>
                )}
              </div>
            ))}
          </div>
        )}
      </Section>

      <Dialog open={!!group} onOpenChange={(o) => !o && setGroup(null)}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-lg">
          {group && (
            <form onSubmit={saveGroup} className="grid gap-4">
              <DialogHeader>
                <DialogTitle>{group.id ? t("editGroup") : t("newGroup")}</DialogTitle>
                <DialogDescription>{t("groupDialogText")}</DialogDescription>
              </DialogHeader>
              <div className="grid gap-1.5">
                <Label htmlFor="g-name">{t("name")}</Label>
                <Input id="g-name" required maxLength={64} value={group.name} onChange={(e) => setGroup({ ...group, name: e.target.value })} placeholder="web" />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="g-desc">{t("description")}</Label>
                <Textarea id="g-desc" rows={2} maxLength={500} value={group.description} onChange={(e) => setGroup({ ...group, description: e.target.value })} />
              </div>
              <div className="grid gap-1.5">
                <Label>{t("monitorsSelected", { count: group.monitor_ids.length })}</Label>
                <div className="grid max-h-60 gap-1 overflow-y-auto sm:grid-cols-2">
                  {all.map((m) => {
                    const other = m.group_id && m.group_id !== group.id ? groupName.get(m.group_id) : undefined
                    return (
                      <label key={m.id} className="flex cursor-pointer items-center gap-2 border border-dashed px-2 py-1.5 hover:bg-accent/50">
                        <Checkbox
                          checked={group.monitor_ids.includes(m.id)}
                          onCheckedChange={(v) =>
                            setGroup({ ...group, monitor_ids: v ? [...group.monitor_ids, m.id] : group.monitor_ids.filter((x) => x !== m.id) })
                          }
                        />
                        <span className="min-w-0 flex-1 truncate">{m.name}</span>
                        {other && <span className="shrink-0 text-[10px] text-muted-foreground">[{other}]</span>}
                      </label>
                    )
                  })}
                  {monitors.data?.length === 0 && <span className="text-xs text-muted-foreground">{t("noMonitorsYet")}</span>}
                </div>
              </div>
              <DialogFooter>
                <Button type="submit">{t("save")}</Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={!!tag} onOpenChange={(o) => !o && setTag(null)}>
        <DialogContent className="sm:max-w-md">
          {tag && (
            <form onSubmit={saveTag} className="grid gap-4">
              <DialogHeader>
                <DialogTitle>{tag.id ? t("editTag") : t("newTag")}</DialogTitle>
                <DialogDescription>{t("tagDialogText")}</DialogDescription>
              </DialogHeader>
              <div className="grid gap-1.5">
                <Label htmlFor="t-name">{t("name")}</Label>
                <Input id="t-name" required maxLength={32} value={tag.name} onChange={(e) => setTag({ ...tag, name: e.target.value })} placeholder={t("tagPlaceholder")} />
              </div>
              <div className="grid gap-1.5">
                <Label>{t("color")}</Label>
                <div className="flex flex-wrap gap-1">
                  {tagColors.map((c) => (
                    <button
                      key={c || "default"}
                      type="button"
                      onClick={() => setTag({ ...tag, color: c })}
                      className={cn("border border-transparent p-0.5", tag.color === c && "border-foreground")}
                      aria-label={c ? t(`colors.${c}`) : t("colors.default")}
                      aria-pressed={tag.color === c}
                    >
                      <TagChip tag={{ name: c ? t(`colors.${c}`) : t("colors.default"), color: c } as Pick<Tag, "name" | "color">} />
                    </button>
                  ))}
                </div>
              </div>
              <div className="text-xs text-muted-foreground">
                {t("preview")} <TagChip tag={{ name: tag.name || t("tag"), color: tag.color }} />
              </div>
              <DialogFooter>
                <Button type="submit">{t("save")}</Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
