"use client"

import { useEffect } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Account } from "@/components/settings/account"
import { TwoFactor } from "@/components/settings/two-factor"
import { useApi, type AuthOptions, type Me } from "@/lib/api"

export default function AccountSettings() {
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 0 })
  const options = useApi<AuthOptions>("/api/auth/options", {
    refreshInterval: 0,
  })
  const t = useTranslations("settings")

  // Result of an OAuth "connect account" round trip.
  useEffect(() => {
    const q = new URLSearchParams(window.location.search)
    const err = q.get("error")
    const linked = q.get("linked")
    if (err) toast.error(err)
    if (linked) toast.success(t("linked", { provider: linked }))
    if (err || linked)
      window.history.replaceState(null, "", window.location.pathname)
  }, [t])

  if (!me.data) return null
  return (
    <>
      <Section title={t("account")}>
        <Account
          me={me.data}
          providers={options.data?.providers ?? []}
          onChange={() => {
            me.mutate()
            options.mutate()
          }}
        />
      </Section>
      <Section title={t("twoFactor")}>
        <TwoFactor me={me.data} onChange={() => me.mutate()} />
      </Section>
    </>
  )
}
