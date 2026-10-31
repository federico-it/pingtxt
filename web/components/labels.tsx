"use client"

import { useMemo, useState } from "react"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { api, useApi, type Group, type Tag, type TagColor } from "@/lib/api"
import { cn } from "@/lib/utils"

// Full class names, so Tailwind keeps them.
const colorClass: Record<TagColor, string> = {
  "": "border-foreground/30 text-foreground/80",
  red: "border-red-500/50 text-red-600 dark:text-red-400",
  orange: "border-orange-500/50 text-orange-600 dark:text-orange-400",
  yellow: "border-yellow-500/60 text-yellow-700 dark:text-yellow-300",
  green: "border-green-500/50 text-green-700 dark:text-green-400",
  cyan: "border-cyan-500/50 text-cyan-700 dark:text-cyan-400",
  blue: "border-blue-500/50 text-blue-600 dark:text-blue-400",
  purple: "border-purple-500/50 text-purple-600 dark:text-purple-400",
  pink: "border-pink-500/50 text-pink-600 dark:text-pink-400",
  gray: "border-zinc-500/50 text-zinc-600 dark:text-zinc-400",
}

const activeClass: Record<TagColor, string> = {
  "": "bg-foreground/10",
  red: "bg-red-500/15",
  orange: "bg-orange-500/15",
  yellow: "bg-yellow-500/20",
  green: "bg-green-500/15",
  cyan: "bg-cyan-500/15",
  blue: "bg-blue-500/15",
  purple: "bg-purple-500/15",
  pink: "bg-pink-500/15",
  gray: "bg-zinc-500/15",
}

export function TagChip({
  tag,
  className,
}: {
  tag: Pick<Tag, "name" | "color">
  className?: string
}) {
  return (
    <span
      className={cn(
        "inline-flex h-4 items-center border px-1 text-[10px] leading-none whitespace-nowrap",
        colorClass[tag.color] ?? colorClass[""],
        className
      )}
    >
      #{tag.name}
    </span>
  )
}

/** A clickable tag that shows whether it is selected. */
export function TagToggle({
  tag,
  on,
  onChange,
}: {
  tag: Pick<Tag, "name" | "color">
  on: boolean
  onChange: (on: boolean) => void
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={() => onChange(!on)}
      className={cn(
        "border border-dashed px-1.5 py-0.5 text-xs transition-colors",
        on
          ? cn(
              "border-solid",
              colorClass[tag.color] ?? colorClass[""],
              activeClass[tag.color]
            )
          : "text-muted-foreground hover:text-foreground"
      )}
    >
      #{tag.name}
    </button>
  )
}

/** Groups and tags with lookup maps, shared by every page that shows them. */
export function useLabels() {
  const groups = useApi<Group[]>("/api/groups", { refreshInterval: 60_000 })
  const tags = useApi<Tag[]>("/api/tags", { refreshInterval: 60_000 })
  const groupById = useMemo(
    () => new Map((groups.data ?? []).map((g) => [g.id, g])),
    [groups.data]
  )
  const tagById = useMemo(
    () => new Map((tags.data ?? []).map((t) => [t.id, t])),
    [tags.data]
  )
  return { groups, tags, groupById, tagById }
}

export function TagList({
  ids,
  tagById,
  className,
}: {
  ids: number[] | undefined
  tagById: Map<number, Tag>
  className?: string
}) {
  const list = (ids ?? [])
    .map((id) => tagById.get(id))
    .filter((t): t is Tag => !!t)
  if (list.length === 0) return null
  return (
    <span
      className={cn(
        "inline-flex flex-wrap items-center gap-1 align-middle",
        className
      )}
    >
      {list.map((t) => (
        <TagChip key={t.id} tag={t} />
      ))}
    </span>
  )
}

/** Toggle tags on and off, with a field to create a new one. */
export function TagPicker({
  value,
  onChange,
  tags,
  onCreated,
}: {
  value: number[]
  onChange: (ids: number[]) => void
  tags: Tag[]
  onCreated?: () => void
}) {
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const tl = useTranslations("labels")

  async function create() {
    const n = name.trim().replace(/^#/, "")
    if (!n) return
    const existing = tags.find((t) => t.name.toLowerCase() === n.toLowerCase())
    if (existing) {
      if (!value.includes(existing.id)) onChange([...value, existing.id])
      setName("")
      return
    }
    setBusy(true)
    try {
      const t = await api<Tag>("/api/tags", {
        method: "POST",
        body: { name: n, color: "" },
      })
      onChange([...value, t.id])
      setName("")
      onCreated?.()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : tl("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-1">
      {tags.map((t) => (
        <TagToggle
          key={t.id}
          tag={t}
          on={value.includes(t.id)}
          onChange={(on) =>
            onChange(on ? [...value, t.id] : value.filter((x) => x !== t.id))
          }
        />
      ))}
      <span className="inline-flex items-center border border-dashed">
        <input
          value={name}
          maxLength={32}
          placeholder={tl("newTag")}
          aria-label={tl("newTag")}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault()
              create()
            }
          }}
          className="h-6 w-24 bg-transparent px-1.5 text-xs outline-none placeholder:text-muted-foreground"
        />
        <button
          type="button"
          aria-label={tl("addTag")}
          disabled={busy || !name.trim()}
          onClick={create}
          className="px-1 text-muted-foreground hover:text-foreground disabled:opacity-40"
        >
          <PlusIcon className="size-3.5" />
        </button>
      </span>
    </div>
  )
}

/** Plain toggle chip for groups. */
export function GroupToggle({
  group,
  on,
  onChange,
}: {
  group: Pick<Group, "name">
  on: boolean
  onChange: (on: boolean) => void
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={() => onChange(!on)}
      className={cn(
        "border border-dashed px-1.5 py-0.5 text-xs transition-colors",
        on
          ? "border-solid border-info bg-info/15 text-foreground"
          : "text-muted-foreground hover:text-foreground"
      )}
    >
      [{group.name}]
    </button>
  )
}
