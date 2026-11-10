"use client"

import { useState } from "react"

import { useT } from "@/components/lang"

// A dark code panel with a green title band and a copy button, always dark
// like a terminal. `cmd` is a one-line variant for the home page steps.
export function CodeCard({ title, code, footer }: { title: string; code: string; footer?: React.ReactNode }) {
  return (
    <div className="border border-edge bg-[#0e100f] text-[#d9e4dc] shadow-[6px_6px_0_0_var(--color-hard)] dark:shadow-[6px_6px_0_0_var(--color-up)]">
      <div className="flex items-center justify-between gap-3 border-b border-edge bg-up px-4 py-2 text-[11px] font-bold uppercase tracking-widest text-[#0e100f]">
        <span className="truncate">{title}</span>
        <Copy code={code} className="border-[#0e100f]/60 text-[#0e100f] hover:bg-[#0e100f]/10" />
      </div>
      <pre className="overflow-x-auto p-4 text-[12.5px] leading-relaxed">
        {code.split("\n").map((l, i) => (
          <div key={i} className={l.trimStart().startsWith("#") ? "text-[#7d8a81]" : undefined}>
            {l || " "}
          </div>
        ))}
      </pre>
      {footer && <div className="border-t border-white/10 px-4 py-3 text-[12px]">{footer}</div>}
    </div>
  )
}

export function Cmd({ code }: { code: string }) {
  return (
    <div className="flex items-center gap-3 bg-[#0e100f] py-2 pr-2 pl-3 text-[12px] text-[#d9e4dc]">
      <pre className="min-w-0 flex-1 overflow-x-auto">
        <span className="text-[#5fd38a]">$</span> {code}
      </pre>
      <Copy code={code} className="border-white/25 text-[#9aa79e] hover:text-white" />
    </div>
  )
}

function Copy({ code, className }: { code: string; className: string }) {
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
    <button
      type="button"
      onClick={copy}
      className={`shrink-0 border px-2 py-0.5 text-[10.5px] font-semibold tracking-wider uppercase transition-colors ${className}`}
    >
      {copied ? t("copied", "copiato") : t("copy", "copia")}
    </button>
  )
}
