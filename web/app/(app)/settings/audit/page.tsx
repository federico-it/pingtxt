"use client"

import { Suspense } from "react"

import { AuditLog } from "@/components/settings/audit-log"
import { AdminOnly } from "@/components/settings/save-bar"

export default function AuditSettingsPage() {
  return (
    <AdminOnly>
      <Suspense>
        <AuditLog />
      </Suspense>
    </AdminOnly>
  )
}
