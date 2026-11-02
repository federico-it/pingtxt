import { Fragment } from "react"

/** Address a visitor can follow: http(s) or mailto, else "". */
export function safeLink(u: string): string {
  const s = u.trim()
  if (/^mailto:[^\s<>"']+$/i.test(s)) return s
  try {
    const p = new URL(s)
    return (p.protocol === "http:" || p.protocol === "https:") && !p.username && !p.password ? s : ""
  } catch {
    return ""
  }
}

// **bold**, *italic*, [text](url)
const tokenRe = /\*\*([^*]+)\*\*|\*([^*\s][^*]*)\*|\[([^\]]+)\]\(([^)\s]+)\)/g

function inline(line: string, linkClass?: string): React.ReactNode[] {
  const out: React.ReactNode[] = []
  let last = 0
  for (const m of line.matchAll(tokenRe)) {
    const i = m.index ?? 0
    if (i > last) out.push(line.slice(last, i))
    if (m[1] !== undefined) out.push(<strong key={i}>{m[1]}</strong>)
    else if (m[2] !== undefined) out.push(<em key={i}>{m[2]}</em>)
    else {
      const href = safeLink(m[4])
      out.push(
        href ? (
          <a key={i} href={href} rel="noopener noreferrer" className={linkClass}>
            {m[3]}
          </a>
        ) : (
          m[0]
        )
      )
    }
    last = i + m[0].length
  }
  if (last < line.length) out.push(line.slice(last))
  return out
}

/**
 * Light Markdown written by the page admin: paragraphs, line breaks,
 * **bold**, *italic* and [links](https://…). Everything else is text.
 */
export function RichText({ text, className, linkClass = "underline underline-offset-4" }: { text: string; className?: string; linkClass?: string }) {
  const paras = text.trim().split(/\n{2,}/)
  return (
    <div className={className}>
      {paras.map((p, i) => (
        <p key={i} className={i > 0 ? "mt-2" : undefined}>
          {p.split("\n").map((l, j) => (
            <Fragment key={j}>
              {j > 0 && <br />}
              {inline(l, linkClass)}
            </Fragment>
          ))}
        </p>
      ))}
    </div>
  )
}
