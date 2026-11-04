"use client"

import { StarIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { togglePin } from "@/lib/pins"
import { cn } from "@/lib/utils"

/** Star that pins a monitor to the top of the list (in this browser). */
export function PinButton({ id, name, pinned, className }: { id: number; name: string; pinned: boolean; className?: string }) {
  const t = useTranslations("dashboard")
  const label = pinned ? t("unpin", { name }) : t("pin", { name })
  return (
    <button
      type="button"
      onClick={() => togglePin(id)}
      aria-pressed={pinned}
      aria-label={label}
      title={label}
      className={cn(
        "flex w-11 shrink-0 items-center justify-center self-stretch text-muted-foreground/60 transition-colors hover:bg-muted/60 hover:text-warn focus-visible:outline-1 focus-visible:outline-ring",
        pinned && "text-warn",
        className
      )}
    >
      <StarIcon aria-hidden className="size-5" strokeWidth={1.75} fill={pinned ? "currentColor" : "none"} />
    </button>
  )
}
