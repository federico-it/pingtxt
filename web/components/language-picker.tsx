"use client"

import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { isLocale, locales, useLocale, type Locale } from "@/lib/i18n"

/** Language menu: the choice is saved in this browser. */
export function LanguagePicker({ className }: { className?: string }) {
  const { locale, setLocale } = useLocale()
  const t = useTranslations("language")
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="sm" className={className} title={t("choose")} aria-label={t("choose")}>
            [{locale}]
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="min-w-36">
        <DropdownMenuRadioGroup value={locale} onValueChange={(v) => isLocale(v) && setLocale(v)}>
          {(Object.keys(locales) as Locale[]).map((l) => (
            <DropdownMenuRadioItem key={l} value={l}>
              <span className="w-6 text-muted-foreground">{l}</span>
              {locales[l]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
