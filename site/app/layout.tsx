import type { Metadata } from "next"
import "@fontsource-variable/jetbrains-mono"

import "./globals.css"
import { SiteShell } from "@/components/site-shell"
import { ThemeProvider } from "@/components/theme"

const description =
  "Minimal, terminal-flavoured, self-hosted uptime monitoring. One container for the hub, tiny agents for multi-location checks."

export const metadata: Metadata = {
  metadataBase: new URL("https://pingtxt.com"),
  title: { default: "pingtxt · minimal self-hosted uptime monitoring", template: "%s · pingtxt" },
  description,
  icons: { icon: "/icon.svg" },
  openGraph: {
    title: "pingtxt",
    description,
    url: "https://pingtxt.com",
    siteName: "pingtxt",
    images: ["/screenshots/dashboard-dark.png"],
    type: "website",
  },
  twitter: { card: "summary_large_image", images: ["/screenshots/dashboard-dark.png"] },
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
