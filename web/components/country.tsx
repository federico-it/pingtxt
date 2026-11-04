"use client"

import { useMemo } from "react"
import { useTranslations } from "use-intl"

import { countryFlag, countryName, countryOptions } from "@/lib/country"
import { useLocale } from "@/lib/i18n"
import { cn } from "@/lib/utils"

/** Flag of a location's country, with the country name as tooltip. Nothing when not set. */
export function CountryFlag({ code, className }: { code?: string; className?: string }) {
  const { locale } = useLocale()
  const flag = countryFlag(code)
  if (!flag || !code) return null
  const name = countryName(code, locale)
  return (
    <span role="img" aria-label={name} title={name} className={cn("mr-1.5 inline-block not-italic", className)}>
      {flag}
    </span>
  )
}

/** Country picker for a location: "none" plus every country, flag and name. */
export function CountrySelect({
  id,
  value,
  onChange,
}: {
  id?: string
  value: string
  onChange: (code: string) => void
}) {
  const t = useTranslations("country")
  const { locale } = useLocale()
  const options = useMemo(() => countryOptions(locale), [locale])
  return (
    <select
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-[13px] dark:bg-input/30"
    >
      <option value="">{t("none")}</option>
      {options.map(([code, name]) => (
        <option key={code} value={code}>
          {countryFlag(code)} {name}
        </option>
      ))}
    </select>
  )
}
