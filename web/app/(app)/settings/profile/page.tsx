"use client"

import { useTheme } from "next-themes"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { option } from "@/components/ui/control"
import { isLocale, locales, useLocale, type Locale } from "@/lib/i18n"

export default function ProfileSettings() {
  const { theme, setTheme } = useTheme()
  const { locale, setLocale } = useLocale()
  const t = useTranslations("settings")
  const tc = useTranslations("common")
  const tl = useTranslations("language")
  return (
    <>
      <Section title={t("appearance")}>
        <div className="flex gap-2">
          {(["system", "light", "dark"] as const).map((th) => (
            <button
              key={th}
              onClick={() => setTheme(th)}
              className={option({ active: theme === th })}
            >
              {tc(`theme.${th}`)}
            </button>
          ))}
        </div>
      </Section>

      <Section title={tl("title")}>
        <div className="flex flex-wrap gap-2">
          {(Object.keys(locales) as Locale[]).map((l) => (
            <button
              key={l}
              lang={l}
              onClick={() => isLocale(l) && setLocale(l)}
              className={option({ active: locale === l })}
            >
              {locales[l]}
            </button>
          ))}
        </div>
        <p className="mt-2 text-[11px] text-muted-foreground">
          {tl.rich("hint", {
            link: (c) => (
              <a
                href="https://crowdin.com/project/pingtxt"
                target="_blank"
                rel="noreferrer"
                className="underline underline-offset-4"
              >
                {c}
              </a>
            ),
          })}
        </p>
      </Section>
    </>
  )
}
