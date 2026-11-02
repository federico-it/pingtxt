import type { PageColors, PageTheme } from "@/lib/api"

export type ColorKey = keyof PageColors
export type Mode = "light" | "dark"

export const colorKeys: ColorKey[] = ["background", "surface", "text", "muted", "border", "accent", "up", "degraded", "down"]

/** The default colors of the hub (globals.css), as #rrggbb for the editor. */
export const defaultColors: Record<Mode, Required<PageColors>> = {
  light: {
    background: "#fafaf9",
    surface: "#efeeec",
    text: "#171613",
    muted: "#6a6966",
    border: "#d8d7d4",
    accent: "#171613",
    up: "#1b9247",
    degraded: "#d48e00",
    down: "#d72828",
  },
  dark: {
    background: "#0b0d0b",
    surface: "#171917",
    text: "#dae0da",
    muted: "#828883",
    border: "#2b2f2c",
    accent: "#6de18b",
    up: "#56d57b",
    degraded: "#f0bb3b",
    down: "#fc5855",
  },
}

/** Font stacks of the bundled fonts (see components/page-fonts.ts). */
export const fontStacks: Record<NonNullable<PageTheme["font"]>, string> = {
  "": `"JetBrains Mono Variable", ui-monospace, SFMono-Regular, Menlo, monospace`,
  plex: `"IBM Plex Sans Variable", system-ui, sans-serif`,
  figtree: `"Figtree Variable", system-ui, sans-serif`,
  atkinson: `"Atkinson Hyperlegible Next Variable", system-ui, sans-serif`,
  serif: `"Source Serif 4 Variable", Georgia, serif`,
  system: `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`,
}

export const fontNames: Record<NonNullable<PageTheme["font"]>, string> = {
  "": "JetBrains Mono",
  plex: "IBM Plex Sans",
  figtree: "Figtree",
  atkinson: "Atkinson Hyperlegible",
  serif: "Source Serif 4",
  system: "system",
}

const hexRe = /^#[0-9a-f]{6}$/

function rgb(hex: string): [number, number, number] {
  return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)) as [number, number, number]
}

function luminance(hex: string) {
  const [r, g, b] = rgb(hex).map((v) => {
    v /= 255
    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** WCAG contrast ratio of two #rrggbb colors (1 to 21). */
export function contrast(a: string, b: string) {
  const x = luminance(a)
  const y = luminance(b)
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05)
}

/** Black or white, whichever reads better on c. */
export function textOn(c: string) {
  return contrast(c, "#ffffff") >= contrast(c, "#111111") ? "#ffffff" : "#111111"
}

/** The colors a page uses in one mode: its own, else the defaults. */
export function resolvedColors(theme: PageTheme | undefined, mode: Mode): Required<PageColors> {
  const own = theme?.[mode] ?? {}
  const out = { ...defaultColors[mode] }
  for (const k of colorKeys) {
    const v = own[k]
    if (v && hexRe.test(v)) out[k] = v
  }
  return out
}

/** CSS custom properties of the hub UI for the colors of one mode. */
function tokens(theme: PageTheme | undefined, mode: Mode): string {
  const own = theme?.[mode] ?? {}
  const c = resolvedColors(theme, mode)
  const v: Record<string, string> = {}
  // Only the colors the page changes: the rest keeps the exact hub values.
  if (own.background)
    Object.assign(v, {
      background: c.background,
      card: c.background,
      popover: c.background,
    })
  if (own.surface)
    Object.assign(v, {
      muted: c.surface,
      secondary: c.surface,
      accent: c.surface,
    })
  if (own.text)
    Object.assign(v, {
      foreground: c.text,
      "card-foreground": c.text,
      "popover-foreground": c.text,
      "secondary-foreground": c.text,
      "accent-foreground": c.text,
    })
  if (own.muted) v["muted-foreground"] = c.muted
  if (own.border) Object.assign(v, { border: c.border, input: c.border })
  if (own.accent)
    Object.assign(v, {
      primary: c.accent,
      "primary-foreground": textOn(c.accent),
      ring: c.accent,
    })
  if (own.up) v.up = c.up
  if (own.degraded) v.warn = c.degraded
  if (own.down) Object.assign(v, { down: c.down, destructive: c.down })
  return Object.entries(v)
    .map(([k, x]) => `--${k}:${x};`)
    .join("")
}

const sizeScale = { "": 1, s: 0.92, l: 1.12 } as const

/**
 * The CSS of a page theme. `scope` is where it applies: ":root" on the
 * public page (the whole document, menus included), ".pt-page" in the
 * editor preview.
 *
 * A forced mode on the public page also switches the html class (see
 * useForcedMode), so the selectors only need the visitor's choice.
 */
export function themeCSS(theme: PageTheme | undefined, scope: ":root" | ".pt-page"): string {
  if (!theme) return ""
  const mode = theme.mode ?? ""
  const light = tokens(theme, "light")
  const dark = tokens(theme, "dark")
  const darkSel = scope === ":root" ? ":root.dark" : ".dark .pt-page"
  const out: string[] = []
  if (scope === ".pt-page" && mode) {
    // The preview forces the mode on its own box, whatever the hub uses.
    const full = (m: Mode) => {
      const c = resolvedColors(theme, m)
      return (
        `--background:${c.background};--card:${c.background};--popover:${c.background};` +
        `--muted:${c.surface};--secondary:${c.surface};--accent:${c.surface};` +
        `--foreground:${c.text};--card-foreground:${c.text};--popover-foreground:${c.text};--secondary-foreground:${c.text};--accent-foreground:${c.text};` +
        `--muted-foreground:${c.muted};--border:${c.border};--input:${c.border};` +
        `--primary:${c.accent};--primary-foreground:${textOn(c.accent)};--ring:${c.accent};` +
        `--up:${c.up};--warn:${c.degraded};--down:${c.down};--destructive:${c.down};`
      )
    }
    out.push(`.pt-page,.dark .pt-page{${full(mode)}color-scheme:${mode}}`)
  } else {
    if (light) out.push(`${scope}{${light}}`)
    if (dark) out.push(`${darkSel}{${dark}}`)
  }
  const s = sizeScale[theme.size ?? ""] ?? 1
  const page: string[] = []
  if (theme.font) page.push(`font-family:${fontStacks[theme.font] ?? fontStacks[""]};font-feature-settings:normal`)
  if (s !== 1)
    page.push(
      `font-size:${(13 * s).toFixed(2)}px`,
      `--text-xs:${(0.75 * s).toFixed(3)}rem`,
      `--text-sm:${(0.875 * s).toFixed(3)}rem`,
      `--text-base:${(1 * s).toFixed(3)}rem`,
      `--text-lg:${(1.125 * s).toFixed(3)}rem`
    )
  if (theme.radius !== undefined && theme.radius !== null) page.push(`--radius:${theme.radius}px;--pt-radius:${theme.radius}px`)
  if (page.length) out.push(`.pt-page{${page.join(";")}}`)
  return out.join("\n")
}

/** Custom CSS as written for the public page, kept inside the box in the preview. */
export function customCSS(theme: PageTheme | undefined, preview: boolean): string {
  const css = theme?.css?.trim()
  if (!css || /<\/style/i.test(css)) return ""
  return preview ? `@scope (.pt-page) {\n${css}\n}` : css
}

/** Ready-made looks. Each sets every option, texts and links stay. */
export const presets: Record<string, PageTheme> = {
  pingtxt: { light: {}, dark: {} },
  clean: {
    light: {
      background: "#ffffff",
      surface: "#f3f4f6",
      text: "#111827",
      muted: "#6b7280",
      border: "#e5e7eb",
      accent: "#2563eb",
    },
    dark: {
      background: "#0b0f17",
      surface: "#151b26",
      text: "#e5e7eb",
      muted: "#9ca3af",
      border: "#263041",
      accent: "#60a5fa",
    },
    font: "plex",
    radius: 6,
    style: "solid",
    bars: "round",
  },
  corporate: {
    mode: "light",
    light: {
      background: "#f7f9fc",
      surface: "#eef2f8",
      text: "#0f1b2d",
      muted: "#5b6a80",
      border: "#d9e0ea",
      accent: "#1e3a8a",
    },
    dark: {},
    font: "plex",
    radius: 2,
    style: "solid",
    density: "compact",
    width: "wide",
    bars: "thin",
    header_style: "band",
  },
  soft: {
    light: {
      background: "#fbfaff",
      surface: "#f1eefb",
      text: "#1f1a2e",
      muted: "#6e6883",
      border: "#e4dff2",
      accent: "#7c3aed",
    },
    dark: {
      background: "#110e19",
      surface: "#1c1828",
      text: "#ebe7f5",
      muted: "#a39cb8",
      border: "#2d2840",
      accent: "#b794f6",
    },
    font: "figtree",
    radius: 16,
    style: "cards",
    density: "airy",
    bars: "round",
  },
  editorial: {
    mode: "light",
    light: {
      background: "#fbf8f2",
      surface: "#f2ece0",
      text: "#221d16",
      muted: "#6f6656",
      border: "#e2d9c7",
      accent: "#b4532a",
    },
    dark: {},
    font: "serif",
    size: "l",
    radius: 0,
    style: "solid",
    density: "airy",
    bars: "thin",
  },
  contrast: {
    light: {
      background: "#ffffff",
      text: "#000000",
      muted: "#3d3d3d",
      border: "#8a8a8a",
      accent: "#0047ab",
      up: "#1f6fb2",
      degraded: "#b85c00",
      down: "#a3122a",
    },
    dark: {
      background: "#000000",
      text: "#ffffff",
      muted: "#d0d0d0",
      border: "#8a8a8a",
      accent: "#7fb6ff",
      up: "#5aa9e6",
      degraded: "#f0a24a",
      down: "#ff8a8a",
    },
    font: "atkinson",
    size: "l",
    style: "solid",
  },
}

/** Status colors that people with red-green color blindness can tell apart. */
export const colorBlindStatus: Record<Mode, Pick<PageColors, "up" | "degraded" | "down">> = {
  light: { up: "#1f6fb2", degraded: "#b85c00", down: "#a3122a" },
  dark: { up: "#5aa9e6", degraded: "#f0a24a", down: "#ff8a8a" },
}

/** Fields of a theme that hold texts, links and switches rather than the look. */
const contentKeys = ["tagline", "header_links", "notice", "footer_text", "footer_links", "hide_powered_by", "logo_favicon", "css"] as const

/** The look fields of a theme: everything but texts and links. */
export function lookOf(t: PageTheme): PageTheme {
  const look: PageTheme = { ...t }
  for (const k of contentKeys) delete look[k]
  return look
}
