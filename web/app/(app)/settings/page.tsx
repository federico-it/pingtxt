"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { useEffect } from "react"
import { useTranslations } from "use-intl"

import {
  legacyAnchors,
  settingsHref,
  useSettingsGroups,
} from "@/lib/settings-nav"

/**
 * /settings/: on phones, the list of categories. On wider screens the
 * sidebar is that list, so this opens the first category. Old links
 * (/settings#mobile-app, OAuth results) are sent on to their category.
 */
export default function SettingsIndex() {
  const router = useRouter()
  const t = useTranslations("settings")
  const groups = useSettingsGroups()

  useEffect(() => {
    const hash = window.location.hash.slice(1)
    const q = new URLSearchParams(window.location.search)
    let to = ""
    if (legacyAnchors[hash]) to = settingsHref(legacyAnchors[hash]) + "#" + hash
    else if (q.has("linked") || q.has("error"))
      to = settingsHref("account") + window.location.search
    else if (window.matchMedia("(min-width: 768px)").matches)
      to = settingsHref("profile")
    if (to) router.replace(to)
  }, [router])

  return (
    <div className="md:hidden">
      {groups.map((g) => (
        <section key={g.key} className="mb-6">
          <div className="mb-2 text-[11px] text-muted-foreground uppercase">
            ── {t(`nav.${g.key}`)}
          </div>
          <div className="divide-y divide-dashed border border-dashed">
            {g.cats.map((slug) => (
              <Link
                key={slug}
                href={settingsHref(slug)}
                className="flex min-h-13 items-center gap-3 px-3 py-2 hover:bg-accent"
              >
                <span className="min-w-0 flex-1">
                  <span className="block">{t(`cats.${slug}`)}/</span>
                  <span className="block text-[11px] text-muted-foreground">
                    {t(`catHints.${slug}`)}
                  </span>
                </span>
                <span className="text-muted-foreground">›</span>
              </Link>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}
