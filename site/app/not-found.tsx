import type { Metadata } from "next"
import Link from "next/link"

import { Prompt } from "@/components/site-shell"

export const metadata: Metadata = {
  title: "page not found",
}

export default function NotFound() {
  return (
    <div className="space-y-4 py-12">
      <Prompt>cat this-page</Prompt>
      <h1 className="text-xl font-semibold">404: no such page</h1>
      <p className="text-muted-foreground">
        It may have moved. Try the{" "}
        <Link href="/" className="text-foreground underline underline-offset-4">
          home page
        </Link>{" "}
        or the{" "}
        <Link href="/docs/" className="text-foreground underline underline-offset-4">
          docs
        </Link>
        .
      </p>
    </div>
  )
}
