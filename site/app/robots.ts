import type { MetadataRoute } from "next"

import { SITE } from "@/lib/seo"

export const dynamic = "force-static"

// pingtxt.com/robots.txt
export default function robots(): MetadataRoute.Robots {
  return {
    rules: { userAgent: "*", allow: "/" },
    sitemap: `${SITE}/sitemap.xml`,
  }
}
