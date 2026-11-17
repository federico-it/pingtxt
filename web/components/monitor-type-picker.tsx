"use client"

import { Combobox } from "@base-ui/react/combobox"
import { CheckIcon, ChevronDownIcon, SearchIcon } from "lucide-react"
import { useMemo } from "react"
import { useTranslations } from "use-intl"

import type { MonitorType } from "@/lib/api"
import { cn } from "@/lib/utils"

/**
 * Monitor types by category, in the order the picker shows them. Every
 * MonitorType must be listed: tsc fails on a missing one (see below).
 */
export const typeCategories = [
  { key: "web", types: ["http", "keyword", "websocket", "grpc", "mcp"] },
  { key: "network", types: ["ping", "tcp", "udp", "dns", "ntp", "ssh"] },
  { key: "email", types: ["smtp", "imap", "pop3", "rbl"] },
  { key: "databases", types: ["postgres", "mysql", "redis", "mongodb"] },
  { key: "servers", types: ["server", "docker"] },
  { key: "services", types: ["statuspage", "push"] },
] as const satisfies readonly { key: string; types: readonly MonitorType[] }[]

// Fails to compile when a MonitorType is missing from typeCategories.
type Listed = (typeof typeCategories)[number]["types"][number]
const allListed: Exclude<MonitorType, Listed> extends never ? true : never = true
void allListed

interface TypeItem {
  value: MonitorType
  label: string
  hint: string
  /** lowercase text the search matches: label, hint, value and category */
  search: string
}

interface TypeGroup {
  value: string
  items: TypeItem[]
}

/** Matches every word of the query anywhere in the item's search text. */
export function matchesType(search: string, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  return words.every((w) => search.includes(w))
}

/**
 * The monitor type selector: types grouped by category, with a search box at
 * the top of the list that filters by name, description or category.
 */
export function MonitorTypePicker({
  value,
  onChange,
}: {
  value: MonitorType
  onChange: (type: MonitorType) => void
}) {
  const tr = useTranslations("monitorForm")

  const groups = useMemo<TypeGroup[]>(
    () =>
      typeCategories.map((c) => {
        const category = tr(`typeCategories.${c.key}`)
        return {
          value: category,
          items: c.types.map((type) => {
            const label = tr(`types.${type}.label`)
            const hint = tr(`types.${type}.hint`)
            return {
              value: type,
              label,
              hint,
              search: `${label} ${type} ${category} ${hint}`.toLowerCase(),
            }
          }),
        }
      }),
    [tr]
  )
  const selected =
    groups.flatMap((g) => g.items).find((i) => i.value === value) ?? null

  return (
    <Combobox.Root
      items={groups}
      value={selected}
      onValueChange={(v) => v && onChange((v as TypeItem).value)}
      isItemEqualToValue={(a: TypeItem, b: TypeItem) => a.value === b.value}
      itemToStringLabel={(i: TypeItem) => i.label}
      itemToStringValue={(i: TypeItem) => i.value}
      filter={(i: TypeItem, q: string) => matchesType(i.search, q)}
      autoHighlight
    >
      <Combobox.Trigger
        aria-label={tr("type")}
        className="flex h-8 w-full items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent pr-2 pl-2.5 text-[13px] whitespace-nowrap transition-colors outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 dark:hover:bg-input/50"
      >
        <span className="line-clamp-1 flex-1 text-left">
          <Combobox.Value />
        </span>
        <ChevronDownIcon className="pointer-events-none size-4 shrink-0 text-muted-foreground" />
      </Combobox.Trigger>
      <Combobox.Portal>
        <Combobox.Positioner
          align="start"
          sideOffset={4}
          className="isolate z-50 outline-none"
        >
          <Combobox.Popup
            aria-label={tr("type")}
            className="flex max-h-[min(26rem,var(--available-height))] w-(--anchor-width) max-w-(--available-width) min-w-64 origin-(--transform-origin) flex-col overflow-hidden rounded-lg bg-popover text-popover-foreground shadow-md ring-1 ring-foreground/10 duration-100 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95"
          >
            <div className="flex items-center gap-1.5 border-b px-2.5">
              <SearchIcon className="size-4 shrink-0 text-muted-foreground" />
              <Combobox.Input
                placeholder={tr("typeSearch")}
                className="h-9 w-full min-w-0 bg-transparent text-base outline-none placeholder:text-muted-foreground md:text-[13px]"
              />
            </div>
            <Combobox.Empty>
              <div className="px-2.5 py-3 text-[13px] text-muted-foreground">
                {tr("typeNoMatch")}
              </div>
            </Combobox.Empty>
            <Combobox.List className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-1 scroll-py-1 outline-0 empty:p-0">
              {(group: TypeGroup) => (
                <Combobox.Group
                  key={group.value}
                  items={group.items}
                  className="pb-1 last:pb-0"
                >
                  <Combobox.GroupLabel className="px-1.5 pt-1.5 pb-1 text-xs text-muted-foreground select-none">
                    {group.value}
                  </Combobox.GroupLabel>
                  <Combobox.Collection>
                    {(item: TypeItem) => (
                      <Combobox.Item
                        key={item.value}
                        value={item}
                        className={cn(
                          "relative grid cursor-default gap-0.5 rounded-md py-1 pr-8 pl-1.5 outline-hidden select-none",
                          "data-highlighted:bg-accent data-highlighted:text-accent-foreground"
                        )}
                      >
                        <span className="text-[13px]">{item.label}</span>
                        <span className="line-clamp-1 text-xs text-muted-foreground">
                          {item.hint}
                        </span>
                        <Combobox.ItemIndicator className="pointer-events-none absolute top-1.5 right-2 flex size-4 items-center justify-center">
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
