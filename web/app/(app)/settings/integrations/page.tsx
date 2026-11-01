"use client"

import { MetricsForm } from "@/components/settings/general-settings"
import { MobileAppSettings } from "@/components/settings/mobile-app"
import { AdminOnly } from "@/components/settings/save-bar"
import { useApi, type Settings } from "@/lib/api"

export default function IntegrationSettingsPage() {
  return (
    <AdminOnly>
      <IntegrationSettings />
    </AdminOnly>
  )
}

function IntegrationSettings() {
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  return (
    <>
      <MobileAppSettings />
      {settings.data && (
        <MetricsForm
          key={JSON.stringify(settings.data.general)}
          settings={settings.data}
          onSaved={(s) => settings.mutate(s, { revalidate: false })}
        />
      )}
    </>
  )
}
