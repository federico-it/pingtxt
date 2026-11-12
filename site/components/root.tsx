import type { Metadata, Viewport } from "next"
import "@fontsource-variable/jetbrains-mono"

import "@/app/globals.css"
import { LangProvider } from "@/components/lang"
import { SiteShell } from "@/components/site-shell"
import { ThemeProvider } from "@/components/theme"
import { LANG_KEY, OG_LOCALE, tx, type Lang } from "@/lib/i18n"
import { NAME, ogImage, SITE } from "@/lib/seo"

// The two root layouts, app/(en)/layout.tsx and app/it/layout.tsx, are this
// with their language, so <html lang> is right on every page.

// Defaults for every page; pages set their own title, description and canonical URL with pageMeta.
export function rootMetadata(lang: Lang): Metadata {
  const t = tx(lang)
  const image = ogImage(lang)
  return {
    metadataBase: new URL(SITE),
    title: {
      default: t(
        `${NAME} · open source, self-hosted uptime monitoring`,
        `${NAME} · monitoraggio uptime open source e self-hosted`,
      ),
      template: `%s · ${NAME}`,
    },
    description: t(
      "Open source, self-hosted uptime monitoring. One Docker container for the hub, tiny agents for multi-location checks, status pages and alerts.",
      "Monitoraggio dell'uptime open source e self-hosted. Un container Docker per l'hub, agent leggeri per i controlli da più località, status page e avvisi.",
    ),
    applicationName: NAME,
    authors: [{ name: "Federico Mengascini" }],
    keywords: t(
      [
        "uptime monitoring",
        "self-hosted",
        "open source",
        "status page",
        "website monitoring",
        "server monitoring",
        "multi-location",
        "docker",
      ],
      [
        "monitoraggio uptime",
        "self-hosted",
        "open source",
        "status page",
        "monitoraggio siti web",
        "monitoraggio server",
        "più località",
        "docker",
      ],
    ),
    icons: { icon: "/icon.svg" },
    openGraph: { siteName: NAME, locale: OG_LOCALE[lang], type: "website", images: [image] },
    twitter: { card: "summary_large_image", images: [image] },
  }
}

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#fbfbf8" },
    { media: "(prefers-color-scheme: dark)", color: "#0e100f" },
  ],
}

// Runs before the page paints. Without a saved choice, a browser that prefers
// Italian over English goes from an English page to the same page in Italian.
// A choice made with the [it] / [en] switch (saved in localStorage) wins both
// ways. Search engines send no language, so they keep the English pages.
const pickLang = `(function(){try{
var p=location.pathname,onIt=p==="/it"||p.indexOf("/it/")===0,saved=null;
try{saved=localStorage.getItem(${JSON.stringify(LANG_KEY)})}catch(e){}
var want=saved;
if(!want){var ls=navigator.languages||[navigator.language];
for(var i=0;i<ls.length;i++){var l=(ls[i]||"").toLowerCase();
if(l.indexOf("it")===0){want="it";break}if(l.indexOf("en")===0){want="en";break}}}
var rest=location.search+location.hash;
if(want==="it"&&!onIt)location.replace("/it"+p+rest);
else if(saved==="en"&&onIt)location.replace((p.slice(3)||"/")+rest);
}catch(e){}})()`

// Rybbit, the website's analytics (pingtxt.com only, never the hub).
const RYBBIT_SCRIPT = "https://rybbit.js.lc/api/script.js?siteId=11b089d40cca"

export function Root({ lang, notFound, children }: { lang: Lang; notFound?: boolean; children: React.ReactNode }) {
  return (
    <html lang={lang} suppressHydrationWarning>
      <head>
        {!notFound && <script dangerouslySetInnerHTML={{ __html: pickLang }} />}
        {/* Cookieless page-view analytics; described in the privacy policy. */}
        <script src={RYBBIT_SCRIPT} defer />
      </head>
      <body>
        <LangProvider lang={lang}>
          <ThemeProvider>
            <SiteShell lang={lang} notFound={notFound}>
              {children}
            </SiteShell>
          </ThemeProvider>
        </LangProvider>
      </body>
    </html>
  )
}
