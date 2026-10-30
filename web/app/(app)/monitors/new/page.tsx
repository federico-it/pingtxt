"use client"

import { emptyMonitor, MonitorForm } from "@/components/monitor-form"
import { PageHeader } from "@/components/page-header"

export default function NewMonitorPage() {
  return (
    <>
      <PageHeader cmd="pingtxt monitors add" title="new monitor" />
      <MonitorForm initial={emptyMonitor} />
    </>
  )
}
