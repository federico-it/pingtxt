"use client"

import { useState } from "react"
import { CheckIcon, CopyIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export function CopyBlock({ code, className }: { code: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  const t = useTranslations("common")
  async function copy() {
    try {
      await navigator.clipboard.writeText(code)
    } catch {
      const ta = document.createElement("textarea")
      ta.value = code
      document.body.appendChild(ta)
      ta.select()
      document.execCommand("copy")
      ta.remove()
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className={cn("group relative border bg-muted/50", className)}>
      <pre className="overflow-x-auto p-3 pr-10 text-xs leading-relaxed whitespace-pre-wrap break-all">{code}</pre>
      <Button variant="ghost" size="icon-xs" className="absolute top-2 right-2" onClick={copy} aria-label={t("copy")}>
        {copied ? <CheckIcon className="text-up" /> : <CopyIcon />}
      </Button>
    </div>
  )
}
