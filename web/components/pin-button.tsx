"use client"

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
        "flex w-8 shrink-0 items-center justify-center text-muted-foreground/50 hover:text-warn",
        pinned && "text-warn",
        className
      )}
    >
      {pinned ? "★" : "☆"}
    </button>
  )
}
