"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useTranslations } from "use-intl"

import { PageHeader } from "@/components/page-header"
import {
  settingsCategories,
  settingsHref,
  useSettingsGroups,
} from "@/lib/settings-nav"
import { cn } from "@/lib/utils"

/** Sidebar with the settings categories; each category is its own page. */
export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const pathname = usePathname() || ""
  const t = useTranslations("settings")
  const current = settingsCategories.find((c) =>
    pathname.startsWith(settingsHref(c.slug))
  )?.slug
  const groups = useSettingsGroups()

  return (
    <>
      {current && (
        <Link
          href="/settings/"
          className="mb-3 inline-flex h-8 items-center text-xs text-muted-foreground hover:text-foreground md:hidden"
        >
          ‹ {t("nav.back")}
        </Link>
      )}
      <PageHeader
        cmd={current ? `pingtxt config ${current}` : "pingtxt config"}
        title={
          current ? (
            <>
              <span className="hidden md:inline">
                {t("title")} <span className="text-muted-foreground">/</span>{" "}
              </span>
              {t(`cats.${current}`)}
            </>
          ) : (
            t("title")
          )
        }
      />
      <div className="flex items-start gap-10">
        <nav
          aria-label={t("nav.label")}
          className="sticky top-20 hidden w-44 shrink-0 md:block"
        >
          {groups.map((g) => (
            <div key={g.key} className="mb-5">
              <div className="mb-1 text-[11px] text-muted-foreground uppercase">
                ── {t(`nav.${g.key}`)}
              </div>
              {g.cats.map((slug) => (
                <Link
                  key={slug}
                  href={settingsHref(slug)}
                  aria-current={slug === current ? "page" : undefined}
                  className={cn(
                    "flex h-8 items-center gap-1.5 px-2 text-[13px] text-muted-foreground transition-colors hover:text-foreground",
                    slug === current && "bg-accent text-foreground"
                  )}
                >
                  <span className="w-2.5 text-primary">
                    {slug === current ? ">" : ""}
                  </span>
                  {t(`cats.${slug}`)}/
                </Link>
              ))}
            </div>
          ))}
        </nav>
        <div className="max-w-2xl min-w-0 flex-1 [&>:first-child]:mt-0 [&>:first-child>section:first-child]:mt-0">
          {children}
        </div>
      </div>
    </>
  )
}
