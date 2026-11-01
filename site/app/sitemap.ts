import type { MetadataRoute } from "next"

import { LANGS, localePath } from "@/lib/i18n"
import { languages, SITE } from "@/lib/seo"

export const dynamic = "force-static"

// pingtxt.com/sitemap.xml. Add new pages here (English path): each one is
// listed once per language, with hreflang links to the others.
const pages: { path: string; priority: number }[] = [
  { path: "/", priority: 1 },
  { path: "/docs/", priority: 0.8 },
  { path: "/docs/api/", priority: 0.6 },
  { path: "/privacy/", priority: 0.2 },
]

export default function sitemap(): MetadataRoute.Sitemap {
  const abs = (l: Record<string, string>) => Object.fromEntries(Object.entries(l).map(([k, v]) => [k, `${SITE}${v}`]))
  return pages.flatMap((p) =>
    LANGS.map((lang) => ({
      url: `${SITE}${localePath(lang, p.path)}`,
      priority: p.priority,
      alternates: { languages: abs(languages(p.path)) },
    })),
  )
}
