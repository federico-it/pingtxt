"use client"

import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { AuthSettingsForm } from "@/components/settings/auth-settings"
import { AdminOnly } from "@/components/settings/save-bar"
import { UsersList } from "@/components/settings/users"
import { useApi, type AuthOptions, type Me, type Settings } from "@/lib/api"

export default function AccessSettingsPage() {
  return (
    <AdminOnly>
      <AccessSettings />
    </AdminOnly>
  )
}

function AccessSettings() {
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  const options = useApi<AuthOptions>("/api/auth/options", {
    refreshInterval: 0,
  })
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 0 })
  const t = useTranslations("settings")
  return (
    <>
      {settings.data && (
        <AuthSettingsForm
          key={JSON.stringify(settings.data.auth)}
          settings={settings.data}
          onSaved={(s) => {
            settings.mutate(s, { revalidate: false })
            options.mutate()
          }}
        />
      )}
      {me.data && (
        <Section title={t("users")}>
          <UsersList meId={me.data.user.id} />
        </Section>
      )}
    </>
  )
}
