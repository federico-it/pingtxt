"use client"

import { useTranslations } from "use-intl"

import { Empty } from "@/components/page-header"
import { useIsAdmin } from "@/lib/api"

/** Renders its children for admins and a notice for read-only users. */
export function AdminOnly({ children }: { children: React.ReactNode }) {
  const isAdmin = useIsAdmin()
  const t = useTranslations("common")
  if (isAdmin === undefined) return null
  if (!isAdmin)
    return <Empty>{t("readOnlyNotice")}</Empty>
  return <>{children}</>
}
