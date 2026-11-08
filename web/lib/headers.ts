/** Request headers as "Name: value" lines, for a textarea. */
export function headersToText(h: Record<string, string> | undefined) {
  return Object.entries(h ?? {})
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n")
}

/** Reads "Name: value" lines back; lines without a colon are skipped. */
export function textToHeaders(t: string) {
  const out: Record<string, string> = {}
  for (const line of t.split("\n")) {
    const i = line.indexOf(":")
    if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return out
}
