"use client"

import { Combobox } from "@base-ui/react/combobox"
import { CheckIcon, ChevronDownIcon, XIcon } from "lucide-react"
import { useMemo, useState } from "react"
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

type CountryGroup = { value: "used" | "all"; items: string[] }

/** Lowercase text without accents, so "turkiye" finds "Türkiye". */
function fold(s: string): string {
  return s.normalize("NFD").replace(/\p{Diacritic}/gu, "").toLowerCase()
}

/**
 * Country picker for a location: a search box over every country (flag and name). Typing filters by the name in the UI
 * language, the English name or the two-letter code. `used` puts those countries first (most used first), under their
 * own heading. The value is the country code, "" for none.
 */
export function CountrySelect({
  id,
  value,
  onChange,
  used,
}: {
  id?: string
  value: string
  onChange: (code: string) => void
  /** country codes to list first, most used first */
  used?: string[]
}) {
  const t = useTranslations("country")
  const { locale } = useLocale()
  const options = useMemo(() => countryOptions(locale), [locale])
  const names = useMemo(() => new Map(options), [options])
  // what a query is matched against: name in the UI language, English name and code
  const haystack = useMemo(
    () => new Map(options.map(([code, name]) => [code, fold(`${name} ${countryName(code, "en")} ${code}`)])),
    [options]
  )
  const groups = useMemo((): CountryGroup[] => {
    const top = (used ?? []).filter((c, i, all) => names.has(c) && all.indexOf(c) === i)
    const rest = options.map(([code]) => code).filter((c) => !top.includes(c))
    return top.length
      ? [
          { value: "used", items: top },
          { value: "all", items: rest },
        ]
      : [{ value: "all", items: rest }]
  }, [options, names, used])
  const label = (code: string) => names.get(code) ?? code
  const flag = countryFlag(value)
  const [query, setQuery] = useState("")
  // While typing, best matches first: the name starts with the query, then a word of it does, then anything else.
  const ranked = useMemo((): CountryGroup[] | undefined => {
    const q = fold(query.trim())
    if (!q || (value && fold(label(value)) === q)) return undefined
    const rank = (code: string) => {
      const name = fold(label(code))
      if (name.startsWith(q) || code.toLowerCase() === q) return 0
      if (name.split(/[\s-]+/).some((w) => w.startsWith(q))) return 1
      return (haystack.get(code) ?? "").includes(q) ? 2 : -1
    }
    return groups
      .map((g) => ({
        ...g,
        items: g.items
          .map((code) => [code, rank(code)] as const)
          .filter(([, r]) => r >= 0)
          .sort((a, b) => a[1] - b[1])
          .map(([code]) => code),
      }))
      .filter((g) => g.items.length > 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, value, groups, haystack, names])

  return (
    <Combobox.Root
      items={groups}
      filteredItems={ranked}
      onInputValueChange={setQuery}
      value={value || null}
      onValueChange={(v) => onChange((v as string | null) ?? "")}
      itemToStringLabel={(code: string) => label(code)}
      filter={(code: string, query: string) => {
        const q = fold(query.trim())
        return !q || (haystack.get(code) ?? "").includes(q)
      }}
      autoHighlight
    >
      <Combobox.InputGroup className="relative">
        {flag && (
          <span aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-base leading-none">
            {flag}
          </span>
        )}
        <Combobox.Input
          id={id}
          placeholder={t("search")}
          className={cn(
            "h-8 w-full min-w-0 rounded-lg border border-input bg-transparent py-1 pr-14 pl-2.5 text-base transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-[13px] dark:bg-input/30",
            flag && "pl-8"
          )}
        />
        <div className="absolute inset-y-0 right-1 flex items-center text-muted-foreground">
          <Combobox.Clear aria-label={t("clear")} className="flex size-6 items-center justify-center rounded-md hover:text-foreground">
            <XIcon className="size-3.5" />
          </Combobox.Clear>
          <Combobox.Trigger aria-label={t("open")} className="flex size-6 items-center justify-center rounded-md hover:text-foreground">
            <ChevronDownIcon className="size-4" />
          </Combobox.Trigger>
        </div>
      </Combobox.InputGroup>
      <Combobox.Portal>
        <Combobox.Positioner sideOffset={4} className="isolate z-50 outline-none">
          <Combobox.Popup className="max-h-(--available-height) w-(--anchor-width) max-w-(--available-width) origin-(--transform-origin) overflow-hidden rounded-lg bg-popover text-popover-foreground shadow-md ring-1 ring-foreground/10 duration-100 data-ending-style:opacity-0 data-starting-style:opacity-0">
            <Combobox.Empty>
              <div className="px-2.5 py-3 text-[13px] text-muted-foreground empty:hidden">{t("noResults")}</div>
            </Combobox.Empty>
            <Combobox.List className="max-h-[min(18rem,var(--available-height))] scroll-py-1 overflow-y-auto overscroll-contain p-1 data-empty:p-0">
              {(group: CountryGroup) => (
                <Combobox.Group key={group.value} items={group.items} className="not-last:mb-1">
                  {groups.length > 1 && (
                    <Combobox.GroupLabel className="px-1.5 py-1 text-xs text-muted-foreground select-none">
                      {t(group.value)}
                    </Combobox.GroupLabel>
                  )}
                  <Combobox.Collection>
                    {(code: string) => (
                      <Combobox.Item
                        key={code}
                        value={code}
                        className="relative flex cursor-default items-center gap-2 rounded-md py-1 pr-8 pl-1.5 text-[13px] outline-none select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground"
                      >
                        <span aria-hidden className="w-5 text-center text-base leading-none">
                          {countryFlag(code)}
                        </span>
                        <span className="truncate">{label(code)}</span>
                        <Combobox.ItemIndicator className="absolute right-2 flex size-4 items-center justify-center">
                          <CheckIcon className="size-4" />
                        </Combobox.ItemIndicator>
                      </Combobox.Item>
                    )}
                  </Combobox.Collection>
                </Combobox.Group>
              )}
            </Combobox.List>
          </Combobox.Popup>
        </Combobox.Positioner>
      </Combobox.Portal>
    </Combobox.Root>
  )
}

/** Countries of the given locations, most used first (ties by first seen). */
export function usedCountries(codes: (string | undefined)[], max = 5): string[] {
  const count = new Map<string, number>()
  for (const c of codes) if (c) count.set(c.toUpperCase(), (count.get(c.toUpperCase()) ?? 0) + 1)
  return [...count.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, max)
    .map(([c]) => c)
}
