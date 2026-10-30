"use client"

import { AdminOnly } from "@/components/admin-only"
import { emptyMonitor, MonitorForm } from "@/components/monitor-form"
import { PageHeader } from "@/components/page-header"
import { useApi, type Settings } from "@/lib/api"

export default function NewMonitorPage() {
  return (
    <AdminOnly>
      <NewMonitor />
    </AdminOnly>
  )
}

function NewMonitor() {
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  const defaults = settings.data?.general.monitor_defaults
  return (
    <>
      <PageHeader cmd="pingtxt monitors add" title="new monitor" />
      {(settings.data || settings.error) && (
        <MonitorForm initial={{ ...emptyMonitor, ...defaults }} />
      )}
    </>
  )
}
