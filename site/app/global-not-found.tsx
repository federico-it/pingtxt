import type { Metadata } from "next"
import Link from "next/link"

import { Root } from "@/components/root"
import { Prompt } from "@/components/site-shell"

// out/404.html, served by GitHub Pages for every unknown URL in both
// languages, so it says it in both.
export const metadata: Metadata = {
  title: "page not found · pagina non trovata · pingtxt",
}

const link = "text-foreground underline underline-offset-4"

export default function GlobalNotFound() {
  return (
    <Root lang="en" notFound>
      <div className="space-y-6 py-12">
        <Prompt>cat this-page</Prompt>
        <div className="space-y-2">
          <h1 className="text-xl font-semibold">404: no such page</h1>
          <p className="text-muted-foreground">
            It may have moved. Try the{" "}
            <Link href="/" className={link}>
              home page
            </Link>{" "}
            or the{" "}
            <Link href="/docs/" className={link}>
              docs
            </Link>
            .
          </p>
        </div>
        <div lang="it" className="space-y-2">
          <h2 className="font-semibold">404: pagina non trovata</h2>
          <p className="text-muted-foreground">
            Forse è stata spostata. Prova la{" "}
            <Link href="/it/" className={link}>
              home page
            </Link>{" "}
            o la{" "}
            <Link href="/it/docs/" className={link}>
              documentazione
            </Link>
            .
          </p>
        </div>
      </div>
    </Root>
  )
}
