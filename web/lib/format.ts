export function pct(v: number | undefined | null, digits = 2): string {
  if (v === undefined || v === null || v < 0) return "--.--%"
  return (v * 100).toFixed(v === 1 ? 0 : digits) + "%"
}

export function ms(v: number | undefined | null): string {
  if (v === undefined || v === null || v <= 0) return "--"
  if (v < 10) return v.toFixed(1) + "ms"
  if (v < 1000) return Math.round(v) + "ms"
  return (v / 1000).toFixed(2) + "s"
}

export function ago(ts: number | undefined | null): string {
  if (!ts) return "never"
  const s = Math.max(0, Math.round((Date.now() - ts) / 1000))
  if (s < 5) return "just now"
  if (s < 60) return s + "s ago"
  if (s < 3600) return Math.floor(s / 60) + "m ago"
  if (s < 86400) return Math.floor(s / 3600) + "h ago"
  return Math.floor(s / 86400) + "d ago"
}

export function duration(msTotal: number): string {
  const s = Math.max(0, Math.round(msTotal / 1000))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d) return `${d}d ${h}h`
  if (h) return `${h}h ${m}m`
  if (m) return `${m}m ${s % 60}s`
  return `${s}s`
}

export function datetime(ts: number): string {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function interval(sec: number): string {
  if (sec >= 86400 && sec % 86400 === 0) return sec / 86400 + "d"
  if (sec % 3600 === 0) return sec / 3600 + "h"
  if (sec % 60 === 0) return sec / 60 + "m"
  return sec + "s"
}
