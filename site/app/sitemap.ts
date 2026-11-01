import type { MetadataRoute } from "next"

import { SITE } from "@/lib/seo"

export const dynamic = "force-static"

// pingtxt.com/sitemap.xml. Add new pages here.
const pages: { path: string; priority: number }[] = [
  { path: "/", priority: 1 },
  { path: "/docs/", priority: 0.8 },
  { path: "/docs/api/", priority: 0.6 },
  { path: "/privacy/", priority: 0.2 },
]

export default function sitemap(): MetadataRoute.Sitemap {
  return pages.map((p) => ({ url: `${SITE}${p.path}`, priority: p.priority }))
}
