"use client"

import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { ApiTokens } from "@/components/settings/api-tokens"
import { ConnectedApps } from "@/components/settings/connected-apps"

export default function TokenSettings() {
  const t = useTranslations("settings")
  const tt = useTranslations("apiTokens")
  return (
    <>
      <Section title={t("apiTokens")}>
        <ApiTokens />
      </Section>
      <Section title={tt("iosSignIns")}>
        <ConnectedApps
          kind="ios"
          empty={<p className="text-xs text-muted-foreground">{tt("noIosSignIns")}</p>}
        />
      </Section>
    </>
  )
}
