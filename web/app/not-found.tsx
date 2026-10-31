"use client"

import Link from "next/link"
import { useTranslations } from "use-intl"

export default function NotFound() {
  const t = useTranslations("notFound")
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-2">
      <div className="text-xs text-muted-foreground">
        <span className="text-primary">$</span> cd {"<path>"}
      </div>
      <div>{t("message")}</div>
      <Link href="/" className="text-primary underline underline-offset-4">
        cd ~
      </Link>
    </div>
  )
}
