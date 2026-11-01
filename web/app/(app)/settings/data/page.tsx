"use client"

import { ConfigTransfer, DatabaseBackups } from "@/components/settings/backups"
import { KumaImport } from "@/components/settings/kuma-import"
import { AdminOnly } from "@/components/settings/save-bar"
import { useApi, type AuthOptions, type Settings } from "@/lib/api"

export default function DataSettingsPage() {
  return (
    <AdminOnly>
      <DataSettings />
    </AdminOnly>
  )
}

function DataSettings() {
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  const options = useApi<AuthOptions>("/api/auth/options", {
    refreshInterval: 0,
  })
  return (
    <>
      <ConfigTransfer
        onImported={() => {
          settings.mutate()
          options.mutate()
        }}
      />
      <KumaImport />
      <DatabaseBackups />
    </>
  )
}
