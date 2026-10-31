import type { Metadata } from "next"
import "@fontsource-variable/jetbrains-mono"

import "./globals.css"
import { HubOffline, ServiceWorker } from "@/components/hub-offline"
import { I18nProvider } from "@/lib/i18n"
import { ThemeProvider } from "@/components/theme-provider"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"

export const metadata: Metadata = {
  title: "pingtxt",
  description: "Minimal uptime monitoring",
  icons: { icon: "/icon.svg" },
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  return (
    <html lang="en" suppressHydrationWarning className="antialiased">
      <body>
        <ThemeProvider>
          <I18nProvider>
            <TooltipProvider delay={150}>{children}</TooltipProvider>
            <Toaster position="bottom-right" />
            <HubOffline />
            <ServiceWorker />
          </I18nProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
