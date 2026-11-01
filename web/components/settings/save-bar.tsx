"use client"

import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { useUnsavedGuard } from "@/hooks/use-unsaved-guard"
import { useApi, type Me } from "@/lib/api"

/**
 * Save bar for a settings form: sticks to the bottom of the screen and shows
 * only while the form has unsaved changes. Put it inside the <form>.
 */
export function SaveBar({
  dirty,
  busy,
  onDiscard,
  children,
}: {
  dirty: boolean
  busy: boolean
  onDiscard: () => void
  /** Extra line in the bar, e.g. a validation warning. */
  children?: React.ReactNode
}) {
  const t = useTranslations("settings")
  useUnsavedGuard(dirty, t("unsavedLeave"))
  if (!dirty) return null
  return (
    <div className="sticky bottom-0 z-20 mt-8 flex flex-wrap items-center justify-between gap-3 border border-warn/50 bg-background/95 px-3 py-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] backdrop-blur">
      <span className="text-xs text-warn">
        ● {t("unsaved")}
        {children}
      </span>
      <div className="flex gap-2">
        <Button type="button" variant="ghost" onClick={onDiscard} disabled={busy}>
          {t("discard")}
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? "..." : t("save")}
        </Button>
      </div>
    </div>
  )
}

/** Renders children for admins only; read-only users get a short note. */
export function AdminOnly({ children }: { children: React.ReactNode }) {
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 0 })
  const t = useTranslations("settings")
  if (!me.data) return null
  if (me.data.user.role !== "admin")
    return <p className="text-xs text-muted-foreground">{t("adminOnly")}</p>
  return <>{children}</>
}
