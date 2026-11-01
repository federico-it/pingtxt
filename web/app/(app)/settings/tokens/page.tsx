"use client"

import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { ApiTokens } from "@/components/settings/api-tokens"

export default function TokenSettings() {
  const t = useTranslations("settings")
  return (
    <Section title={t("apiTokens")}>
      <ApiTokens />
    </Section>
  )
}
