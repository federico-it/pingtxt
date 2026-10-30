"use client"

import { Empty } from "@/components/page-header"
import { useIsAdmin } from "@/lib/api"

/** Renders its children for admins and a notice for read-only users. */
export function AdminOnly({ children }: { children: React.ReactNode }) {
  const isAdmin = useIsAdmin()
  if (isAdmin === undefined) return null
  if (!isAdmin)
    return (
      <Empty>your account is read-only. ask an admin to make changes.</Empty>
    )
  return <>{children}</>
}
