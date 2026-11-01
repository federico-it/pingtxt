import type { Metadata } from "next"

import { REPO } from "@/components/site-shell"

export const SITE = "https://pingtxt.com"
export const NAME = "pingtxt"

export const OG_IMAGE = {
  url: "/screenshots/dashboard-dark.png",
  width: 1280,
  height: 860,
  alt: "pingtxt dashboard with monitors, uptime bars and response times",
}

// Metadata for one page: canonical URL plus Open Graph and Twitter cards.
// Next replaces openGraph/twitter of the layout instead of merging them, so
// every page sets them in full. `path` ends with a slash (trailingSlash).
export function pageMeta({
  title,
  description,
  path,
  absoluteTitle,
}: {
  title: string
  description: string
  path: string
  absoluteTitle?: boolean
}): Metadata {
  const full = absoluteTitle ? title : `${title} · ${NAME}`
  return {
    title: absoluteTitle ? { absolute: title } : title,
    description,
    alternates: { canonical: path },
    openGraph: {
      title: full,
      description,
      url: path,
      siteName: NAME,
      locale: "en_US",
      type: "website",
      images: [OG_IMAGE],
    },
    twitter: { card: "summary_large_image", title: full, description, images: [OG_IMAGE] },
  }
}

// schema.org data for the home page, so search engines know pingtxt is a
// free, open source application and not an online service.
export const softwareJsonLd = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "WebSite",
      "@id": `${SITE}/#website`,
      url: `${SITE}/`,
      name: NAME,
    },
    {
      "@type": "SoftwareApplication",
      "@id": `${SITE}/#software`,
      name: NAME,
      url: `${SITE}/`,
      description:
        "Open source, self-hosted uptime monitoring: HTTP, TCP, ping, DNS, database and more checks from multiple locations, status pages and alerts, in one Docker container.",
      applicationCategory: "DeveloperApplication",
      applicationSubCategory: "Uptime monitoring",
      operatingSystem: "Linux, Docker",
      license: "https://www.gnu.org/licenses/agpl-3.0.html",
      isAccessibleForFree: true,
      offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
      sameAs: [REPO],
      image: `${SITE}${OG_IMAGE.url}`,
      screenshot: `${SITE}${OG_IMAGE.url}`,
    },
  ],
}

// JSON for a <script type="application/ld+json">, safe to inline in HTML.
export function jsonLd(data: unknown) {
  return { __html: JSON.stringify(data).replace(/</g, "\\u003c") }
}
