"use client"

import { useState } from "react"

import { useT } from "@/components/lang"

// A shell snippet with a copy button. Lines starting with "#" are shown dimmed.
export function CopyBlock({ code, label }: { code: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  const t = useT()
  async function copy() {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {}
  }
  return (
    <div className="group relative border border-dashed bg-muted/50">
      {label && (
        <div className="border-b border-dashed px-3 py-1.5 text-[11px] uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
      )}
      <pre className="overflow-x-auto p-3 pr-20 text-[12px] leading-relaxed">
        {code.split("\n").map((l, i) => (
          <div key={i} className={l.trimStart().startsWith("#") ? "text-muted-foreground" : undefined}>
            {l || " "}
          </div>
        ))}
      </pre>
      <button
        type="button"
        onClick={copy}
        className="absolute right-2 bottom-2 border border-dashed bg-background px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
      >
        {copied ? t("copied", "copiato") : t("copy", "copia")}
      </button>
    </div>
  )
}
