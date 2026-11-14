"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { GlobeIcon, ImageUpIcon, SearchIcon, XIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { useApi, type DashboardIcon, type Monitor } from "@/lib/api"
import { cn } from "@/lib/utils"

type IconSource = Pick<Monitor, "name" | "icon" | "icon_url" | "icon_dark_url">

/** Picture of an icon with its dark theme variant, if any. */
function IconImg({ src, dark, className }: { src: string; dark?: string; className?: string }) {
  const img = "size-full object-contain"
  return (
    <span className={cn("inline-flex shrink-0 items-center justify-center", className)}>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src={src} alt="" loading="lazy" decoding="async" className={cn(img, dark && "dark:hidden")} />
      {dark && (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={dark} alt="" loading="lazy" decoding="async" className={cn(img, "hidden dark:block")} />
      )}
    </span>
  )
}

/**
 * The icon of a monitor: its image, else its initial on a muted tile (so
 * names stay aligned), nothing with icon "none" but the same room.
 */
export function MonitorIcon({ m, className }: { m: IconSource; className?: string }) {
  const size = cn("size-5", className)
  if (m.icon_url) {
    // Favicons and uploads are made for light backgrounds (a black GitHub
    // logo): on dark themes they sit on a light tile, unless the icon has
    // its own dark variant. Dashboard Icons work on both.
    const tile = !m.icon_dark_url && !m.icon?.startsWith("dashboard:")
    return (
      <IconImg
        src={m.icon_url}
        dark={m.icon_dark_url || undefined}
        className={cn(size, tile && "dark:rounded-[3px] dark:bg-white/90 dark:p-px")}
      />
    )
  }
  if (m.icon === "none") return <span aria-hidden className={cn("shrink-0", size)} />
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex shrink-0 items-center justify-center bg-muted text-[10px] font-medium text-muted-foreground uppercase",
        size
      )}
    >
      {[...m.name.trim()][0] ?? "?"}
    </span>
  )
}

/** Preview path of a Dashboard Icons icon (through the hub). */
function diSrc(name: string, dark = false) {
  return `/api/dashboard-icons/${encodeURIComponent(name)}${dark ? "?theme=dark" : ""}`
}

export type IconChoice = {
  /** Monitor.icon to save */
  icon: string
  /** image chosen for upload, sent after saving */
  file?: File
}

/**
 * Icon field of the monitor form: automatic (favicon), a Dashboard Icons
 * icon from a searchable grid, an uploaded image, or none.
 */
export function MonitorIconPicker({
  value,
  onChange,
  monitor,
  name,
}: {
  value: IconChoice
  onChange: (v: IconChoice) => void
  /** the saved monitor, for its current image (none while creating) */
  monitor?: IconSource
  /** the monitor's name, searched first */
  name: string
}) {
  const t = useTranslations("monitorIcon")
  const [open, setOpen] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const fileURL = useMemo(() => (value.file ? URL.createObjectURL(value.file) : undefined), [value.file])
  useEffect(() => () => (fileURL ? URL.revokeObjectURL(fileURL) : undefined), [fileURL])

  const saved = monitor && (monitor.icon ?? "") === value.icon && !value.file ? monitor : undefined
  const di = value.icon.startsWith("dashboard:") ? value.icon.slice("dashboard:".length) : ""
  let preview: React.ReactNode
  let label: string
  if (value.file && fileURL) {
    preview = <IconImg src={fileURL} className="size-8" />
    label = t("upload")
  } else if (saved?.icon_url) {
    preview = <MonitorIcon m={saved} className="size-8" />
    label = di ? di : value.icon === "custom" ? t("custom") : t("auto")
  } else if (di) {
    preview = <IconImg src={diSrc(di)} dark={diSrc(di, true)} className="size-8" />
    label = di
  } else if (value.icon === "none") {
    preview = <span className="inline-flex size-8 items-center justify-center border border-dashed text-muted-foreground"><XIcon className="size-4" /></span>
    label = t("none")
  } else {
    preview = <span className="inline-flex size-8 items-center justify-center bg-muted text-muted-foreground"><GlobeIcon className="size-4" /></span>
    label = t("auto")
  }
  const hint =
    value.file || value.icon === "custom"
      ? t("customHint")
      : di
        ? t("dashboardHint")
        : value.icon === "none"
          ? t("noneHint")
          : saved?.icon_url
            ? t("autoFound")
            : monitor && (monitor.icon ?? "") === ""
              ? t("autoMissing")
              : t("autoHint")

  return (
    <div className="grid gap-1.5">
      <div className="flex items-center gap-3">
        {preview}
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm">{label}</div>
          <p className="text-[11px] text-muted-foreground">{hint}</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => setOpen(true)}>
          {t("change")}
        </Button>
      </div>
      <input
        ref={fileRef}
        type="file"
        accept="image/svg+xml,image/png,image/jpeg,image/gif,image/webp,image/avif,image/x-icon,.ico"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0]
          e.target.value = ""
          if (f) {
            onChange({ icon: "custom", file: f })
            setOpen(false)
          }
        }}
      />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("title")}</DialogTitle>
            <DialogDescription>{t("description")}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant={value.icon === "" && !value.file ? "default" : "outline"}
              size="sm"
              onClick={() => {
                onChange({ icon: "" })
                setOpen(false)
              }}
            >
              <GlobeIcon /> {t("auto")}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => fileRef.current?.click()}>
              <ImageUpIcon /> {t("uploadButton")}
            </Button>
            <Button
              type="button"
              variant={value.icon === "none" ? "default" : "outline"}
              size="sm"
              onClick={() => {
                onChange({ icon: "none" })
                setOpen(false)
              }}
            >
              <XIcon /> {t("none")}
            </Button>
          </div>
          {open && (
            <DashboardIconGrid
              initialQuery={name}
              selected={di}
              onPick={(name) => {
                onChange({ icon: `dashboard:${name}` })
                setOpen(false)
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

/** Searchable grid of Dashboard Icons. */
function DashboardIconGrid({
  initialQuery,
  selected,
  onPick,
}: {
  initialQuery: string
  selected: string
  onPick: (name: string) => void
}) {
  const t = useTranslations("monitorIcon")
  const [query, setQuery] = useState(selected || firstWord(initialQuery))
  const [q, setQ] = useState(query)
  useEffect(() => {
    const id = setTimeout(() => setQ(query.trim()), 200)
    return () => clearTimeout(id)
  }, [query])
  const res = useApi<{ icons: DashboardIcon[]; total: number }>(
    `/api/dashboard-icons?limit=48&q=${encodeURIComponent(q)}`,
    { refreshInterval: 0, revalidateOnFocus: false, keepPreviousData: true }
  )
  return (
    <div className="grid gap-2">
      <div className="relative">
        <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("search")}
          aria-label={t("search")}
          className="pl-8"
        />
      </div>
      {res.error ? (
        <p className="text-xs text-down">! {res.error.message}</p>
      ) : !res.data ? (
        <p className="text-xs text-muted-foreground">…</p>
      ) : res.data.icons.length === 0 ? (
        <p className="text-xs text-muted-foreground">{t("noResults")}</p>
      ) : (
        <>
          <div className="grid max-h-80 grid-cols-4 gap-1 overflow-y-auto sm:grid-cols-6">
            {res.data.icons.map((i) => (
              <button
                key={i.name}
                type="button"
                title={[i.name, ...i.aliases].join(" · ")}
                onClick={() => onPick(i.name)}
                className={cn(
                  "flex min-w-0 flex-col items-center gap-1 border border-transparent p-2 text-[10px] text-muted-foreground outline-none hover:border-dashed hover:border-border hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50",
                  selected === i.name && "border-dashed border-foreground/40 bg-accent text-foreground"
                )}
              >
                <IconImg src={diSrc(i.name)} dark={i.dark ? diSrc(i.name, true) : undefined} className="size-8" />
                <span className="w-full truncate text-center">{i.name}</span>
              </button>
            ))}
          </div>
          <p className="text-[11px] text-muted-foreground">
            {res.data.total > res.data.icons.length
              ? t("more", { shown: res.data.icons.length, total: res.data.total })
              : null}{" "}
            {t.rich("credit", {
              link: (c) => (
                <a href="https://dashboardicons.com" target="_blank" rel="noreferrer" className="underline">
                  {c}
                </a>
              ),
            })}
          </p>
        </>
      )}
    </div>
  )
}

/** The first word of a monitor's name, a good first search ("Proxmox backup" → proxmox). */
function firstWord(s: string) {
  return s.trim().split(/[\s·:/|-]+/)[0]?.toLowerCase() ?? ""
}
