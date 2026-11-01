import type { Metadata, Viewport } from "next"
import "@fontsource-variable/jetbrains-mono"

import "./globals.css"
import { SiteShell } from "@/components/site-shell"
import { ThemeProvider } from "@/components/theme"
import { NAME, OG_IMAGE, SITE } from "@/lib/seo"

const description =
  "Open source, self-hosted uptime monitoring. One Docker container for the hub, tiny agents for multi-location checks, status pages and alerts."

// Defaults for every page; pages set their own title, description and canonical URL with pageMeta.
export const metadata: Metadata = {
  metadataBase: new URL(SITE),
  title: { default: `${NAME} · open source, self-hosted uptime monitoring`, template: `%s · ${NAME}` },
  description,
  applicationName: NAME,
  authors: [{ name: "Federico Mengascini" }],
  keywords: [
    "uptime monitoring",
    "self-hosted",
    "open source",
    "status page",
    "website monitoring",
    "server monitoring",
    "multi-location",
    "docker",
  ],
  icons: { icon: "/icon.svg" },
  openGraph: { siteName: NAME, locale: "en_US", type: "website", images: [OG_IMAGE] },
  twitter: { card: "summary_large_image", images: [OG_IMAGE] },
}

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#fbfbf8" },
    { media: "(prefers-color-scheme: dark)", color: "#0e100f" },
  ],
}

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body>
        <ThemeProvider>
          <SiteShell>{children}</SiteShell>
        </ThemeProvider>
      </body>
    </html>
  )
}
