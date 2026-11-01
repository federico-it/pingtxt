"use client"

import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import {
  InstanceForm,
  UsageStats,
} from "@/components/settings/general-settings"
import { AdminOnly } from "@/components/settings/save-bar"
import { useApi, type Me, type Settings } from "@/lib/api"

export default function GeneralSettingsPage() {
  return (
    <AdminOnly>
      <General />
    </AdminOnly>
  )
}

function General() {
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 0 })
  const t = useTranslations("settings")
  if (!settings.data) return null
  return (
    <>
      <InstanceForm
        key={JSON.stringify(settings.data.general)}
        settings={settings.data}
        onSaved={(s) => settings.mutate(s, { revalidate: false })}
      />
      <UsageStats />
      <Section title={t("about")}>
        <pre className="text-xs text-muted-foreground">
          {`version    ${me.data?.version ?? "--"}
source     github.com/federico-it/pingtxt`}
        </pre>
      </Section>
    </>
  )
}
