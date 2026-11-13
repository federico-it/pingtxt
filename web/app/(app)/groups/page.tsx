"use client"

import Link from "next/link"
import { useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { TagChip, useLabels } from "@/components/labels"
import { Empty, PageHeader, Section } from "@/components/page-header"
import { StatusGlyph } from "@/components/status"
import { TreeGuides } from "@/components/tree-guides"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { api, tagColors, useApi, useIsAdmin, type Group, type Monitor, type Tag, type TagColor } from "@/lib/api"
import { MAX_GROUP_DEPTH } from "@/lib/groups"
import { cn } from "@/lib/utils"

type GroupDraft = { id?: number; name: string; description: string; parent_id: number; monitor_ids: number[] }
type TagDraft = { id?: number; name: string; color: TagColor }

export default function GroupsPage() {
  const { groups, tags, tree } = useLabels()
  const isAdmin = useIsAdmin()
  const t = useTranslations("groups")
  const monitors = useApi<Monitor[]>("/api/monitors", { refreshInterval: 30_000 })
  const [group, setGroup] = useState<GroupDraft | null>(null)
  const [tag, setTag] = useState<TagDraft | null>(null)
  // Groups whose subgroups are folded away on this page.
  const [folded, setFolded] = useState<Set<number>>(new Set())
  // Filter for the monitor list in the group dialog.
  const [pick, setPick] = useState("")

  const all = monitors.data ?? []

  function refresh() {
    groups.mutate()
    tags.mutate()
    monitors.mutate()
  }

  function editGroup(g?: Group, parent = 0) {
    setPick("")
    setGroup(
      g
        ? { id: g.id, name: g.name, description: g.description, parent_id: g.parent_id ?? 0, monitor_ids: all.filter((m) => m.group_id === g.id).map((m) => m.id) }
        : { name: "", description: "", parent_id: parent, monitor_ids: [] }
    )
  }

  async function saveGroup(e: React.FormEvent) {
    e.preventDefault()
    if (!group) return
    try {
      const saved = await api<Group>(group.id ? `/api/groups/${group.id}` : "/api/groups", {
        method: group.id ? "PUT" : "POST",
        body: { name: group.name, description: group.description, parent_id: group.parent_id },
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

  const groupName = new Map(tree.ordered.map((g) => [g.id, g.path]))
  // Groups the one being edited can move under: not itself or its
  // subgroups, and within the nesting limit with its own subgroups.
  const parentOptions = group
    ? tree.ordered.filter((g) => {
        if (!group.id) return g.depth + 1 < MAX_GROUP_DEPTH
        if (g.id === group.id || g.ancestors.includes(group.id)) return false
        return g.depth + 1 + tree.height(group.id) <= MAX_GROUP_DEPTH
      })
    : []

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
            {tree.ordered
              .filter((g) => !g.ancestors.some((a) => folded.has(a)))
              .map((g) => {
              const members = all.filter((m) => m.group_id === g.id)
              const subgroups = tree.descendants(g.id).length
              const children = tree.ordered.filter((c) => c.ancestors[0] === g.id).length
              const isFolded = folded.has(g.id)
              return (
                <div key={g.id} className={cn("flex items-stretch border-b border-dashed", g.depth === 0 && "bg-muted/30")}>
                  <TreeGuides depth={g.depth} />
                  <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1 py-2.5 pr-2 pl-2">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-baseline gap-x-3">
                      <button
                        type="button"
                        disabled={children === 0}
                        onClick={() =>
                          setFolded((p) => {
                            const next = new Set(p)
                            if (next.has(g.id)) next.delete(g.id)
                            else next.add(g.id)
                            return next
                          })
                        }
                        aria-expanded={children ? !isFolded : undefined}
                        aria-label={children ? (isFolded ? t("unfold", { name: g.name }) : t("fold", { name: g.name })) : undefined}
                        className="w-3 shrink-0 text-left text-muted-foreground disabled:opacity-40"
                      >
                        {children === 0 ? "·" : isFolded ? "▸" : "▾"}
                      </button>
                      <span className={g.depth === 0 ? "font-semibold" : "font-medium"}>{g.name}</span>
                      {subgroups > 0 && (
                        <span className="text-xs text-muted-foreground">{t("subgroupCount", { count: subgroups })}</span>
                      )}
                      <Link href={`/?group=${g.id}`} className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
                        {t("monitorCount", { count: g.monitors })}
                      </Link>
                    </div>
                    {g.description && <div className="truncate pl-6 text-xs text-muted-foreground">{g.description}</div>}
                    {members.length > 0 && (
                      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 pl-6 text-xs text-muted-foreground">
                        {members.map((m) => (
                          <Link key={m.id} href={`/monitors/view/?id=${m.id}`} className="inline-flex items-center gap-1 hover:text-foreground">
                            <StatusGlyph status={m.status} />
                            {m.name}
                          </Link>
                        ))}
                      </div>
                    )}
                  </div>
                  {isAdmin && (
                    <div className="flex shrink-0 items-center">
                      {g.depth + 1 < MAX_GROUP_DEPTH && (
                        <Button variant="ghost" size="xs" onClick={() => editGroup(undefined, g.id)} title={t("addSubgroupTo", { name: g.name })}>
                          <PlusIcon /> {t("subgroup")}
                        </Button>
                      )}
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
                    </div>
                  )}
                  </div>
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
                <Label htmlFor="g-parent">{t("parent")}</Label>
                <select
                  id="g-parent"
                  value={String(group.parent_id)}
                  onChange={(e) => setGroup({ ...group, parent_id: Number(e.target.value) })}
                  className="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2 py-1 text-base outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-[13px] dark:bg-input/30"
                >
                  <option value="0">{t("noParent")}</option>
                  {parentOptions.map((g) => (
                    <option key={g.id} value={g.id}>
                      {"\u00a0\u00a0".repeat(g.depth)}
                      {g.depth > 0 ? "└ " : ""}
                      {g.name}
                    </option>
                  ))}
                </select>
                <span className="text-xs text-muted-foreground">{t("parentHint", { max: MAX_GROUP_DEPTH })}</span>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="g-desc">{t("description")}</Label>
                <Textarea id="g-desc" rows={2} maxLength={500} value={group.description} onChange={(e) => setGroup({ ...group, description: e.target.value })} />
              </div>
              <div className="grid gap-1.5">
                <div className="flex items-center gap-2">
                  <Label className="mr-auto">{t("monitorsSelected", { count: group.monitor_ids.length })}</Label>
                  {all.length > 8 && (
                    <Input
                      value={pick}
                      onChange={(e) => setPick(e.target.value)}
                      placeholder={t("findMonitor")}
                      aria-label={t("findMonitor")}
                      className="h-7 w-40"
                    />
                  )}
                </div>
                <div className="grid max-h-60 gap-1 overflow-y-auto sm:grid-cols-2">
                  {all
                    .filter((m) => !pick || m.name.toLowerCase().includes(pick.toLowerCase()) || group.monitor_ids.includes(m.id))
                    // the group's saved monitors first (fixed while ticking), then by name
                    .sort((a, b) => Number(!!group.id && b.group_id === group.id) - Number(!!group.id && a.group_id === group.id) || a.name.localeCompare(b.name))
                    .map((m) => {
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
