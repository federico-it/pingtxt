"use client"

import { MonitorDefaultsForm } from "@/components/settings/general-settings"
import { AdminOnly } from "@/components/settings/save-bar"
import { useApi, type Settings } from "@/lib/api"

export default function MonitorSettingsPage() {
  return (
    <AdminOnly>
      <MonitorSettings />
    </AdminOnly>
  )
}

function MonitorSettings() {
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  if (!settings.data) return null
  return (
    <MonitorDefaultsForm
      key={JSON.stringify(settings.data.general)}
      settings={settings.data}
      onSaved={(s) => settings.mutate(s, { revalidate: false })}
    />
  )
}
