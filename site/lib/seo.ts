import type { Metadata } from "next"

import { REPO } from "@/components/site-shell"
import { LANGS, localePath, OG_LOCALE, tx, type Lang } from "@/lib/i18n"

export const SITE = "https://pingtxt.com"
export const NAME = "pingtxt"

export function ogImage(lang: Lang) {
  return {
    url: "/screenshots/dashboard-dark.png",
    width: 1280,
    height: 860,
    alt: tx(lang)(
      "pingtxt dashboard with monitors, uptime bars and response times",
      "dashboard di pingtxt con monitor, barre di uptime e tempi di risposta",
    ),
  }
}

// hreflang links of a page that exists in every language. `path` is the
// English one ("/docs/"); x-default is English too.
export function languages(path: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const l of LANGS) out[l] = localePath(l, path)
  out["x-default"] = path
  return out
}

// Metadata for one page: canonical URL, hreflang alternates, Open Graph and
// Twitter cards. Next replaces openGraph/twitter of the layout instead of
// merging them, so every page sets them in full. `path` is the English path
// and ends with a slash (trailingSlash); the Italian one gets /it in front.
export function pageMeta({
  lang,
  title,
  description,
  path,
  absoluteTitle,
}: {
  lang: Lang
  title: string
  description: string
  path: string
  absoluteTitle?: boolean
}): Metadata {
  const full = absoluteTitle ? title : `${title} · ${NAME}`
  const url = localePath(lang, path)
  const image = ogImage(lang)
  return {
    title: absoluteTitle ? { absolute: title } : title,
    description,
    alternates: { canonical: url, languages: languages(path) },
    openGraph: {
      title: full,
      description,
      url,
      siteName: NAME,
      locale: OG_LOCALE[lang],
      alternateLocale: LANGS.filter((l) => l !== lang).map((l) => OG_LOCALE[l]),
      type: "website",
      images: [image],
    },
    twitter: { card: "summary_large_image", title: full, description, images: [image] },
  }
}

// schema.org data for the home page, so search engines know pingtxt is a
// free, open source application and not an online service.
export function softwareJsonLd(lang: Lang) {
  const t = tx(lang)
  const home = `${SITE}${localePath(lang, "/")}`
  const image = `${SITE}${ogImage(lang).url}`
  return {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "WebSite",
        "@id": `${SITE}/#website`,
        url: `${SITE}/`,
        name: NAME,
        inLanguage: [...LANGS],
      },
      {
        "@type": "SoftwareApplication",
        "@id": `${SITE}/#software`,
        name: NAME,
        url: home,
        inLanguage: lang,
        description: t(
          "Open source, self-hosted uptime monitoring: HTTP, TCP, ping, DNS, database and more checks from multiple locations, status pages and alerts, in one Docker container.",
          "Monitoraggio dell'uptime open source e self-hosted: controlli HTTP, TCP, ping, DNS, database e altro da più località, status page e avvisi, in un solo container Docker.",
        ),
        applicationCategory: "DeveloperApplication",
        applicationSubCategory: t("Uptime monitoring", "Monitoraggio dell'uptime"),
        operatingSystem: "Linux, Docker",
        license: "https://www.gnu.org/licenses/agpl-3.0.html",
        isAccessibleForFree: true,
        offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
        sameAs: [REPO],
        image,
        screenshot: image,
      },
    ],
  }
}

// JSON for a <script type="application/ld+json">, safe to inline in HTML.
export function jsonLd(data: unknown) {
  return { __html: JSON.stringify(data).replace(/</g, "\\u003c") }
}
