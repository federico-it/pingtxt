"use client"

import { PlusIcon, RotateCcwIcon, XIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { option, segment } from "@/components/ui/control"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import type { PageColors, PageLink, PageTheme } from "@/lib/api"
import { colorBlindStatus, colorKeys, contrast, defaultColors, fontNames, lookOf, presets, resolvedColors, type ColorKey, type Mode } from "@/lib/page-theme"
import { cn } from "@/lib/utils"

const radii = [0, 2, 4, 6, 8, 12, 16, 24]

/** Texts, links and switches of a theme: what a preset keeps. */
function contentOf(t: PageTheme): PageTheme {
  const look = lookOf(t)
  const out: PageTheme = { ...t }
  for (const k of Object.keys(look) as (keyof PageTheme)[]) delete out[k]
  return out
}

/** Which preset the theme matches, "" when it was changed by hand. */
function presetOf(t: PageTheme): string {
  const look = JSON.stringify(normLook(t))
  return Object.keys(presets).find((k) => JSON.stringify(normLook(presets[k])) === look) ?? ""
}

function normLook(t: PageTheme) {
  const l = lookOf(t)
  const clean = (c?: PageColors) =>
    Object.fromEntries(
      Object.entries(c ?? {})
        .filter(([, v]) => v)
        .sort()
    )
  return Object.fromEntries(
    Object.entries({ ...l, light: clean(l.light), dark: clean(l.dark) })
      .filter(([, v]) => v !== "" && v !== undefined && v !== null)
      .sort()
  )
}

/**
 * The "look" section of the status page editor: preset, colors, font,
 * shape, header, footer and custom CSS. The preview next to the form shows
 * every change.
 */
export function PageThemeFields({ value, onChange, hasLogo }: { value: PageTheme; onChange: (t: PageTheme) => void; hasLogo: boolean }) {
  const t = useTranslations("statusPageTheme")
  // Keys built from option values ("modes.dark", "styles.cards"...).
  const tk = t as unknown as (key: string) => string
  const th = value
  const set = <K extends keyof PageTheme>(k: K, v: PageTheme[K]) => onChange({ ...th, [k]: v })
  const setColor = (mode: Mode, k: ColorKey, v: string) => onChange({ ...th, [mode]: { ...(th[mode] ?? {}), [k]: v } })
  const current = presetOf(th)

  return (
    <Section title={t("title")}>
      <div className="grid gap-5">
        <Field label={t("preset")} hint={t("presetHint")}>
          <div className="flex flex-wrap gap-2">
            {Object.keys(presets).map((k) => (
              <button
                key={k}
                type="button"
                className={option({ active: current === k })}
                onClick={() => onChange({ ...contentOf(th), ...structuredClone(presets[k]) })}
              >
                {tk(`presets.${k}`)}
              </button>
            ))}
          </div>
        </Field>

        <Choice
          label={t("mode")}
          value={th.mode ?? ""}
          options={["", "light", "dark"]}
          labels={(v) => tk(`modes.${v || "auto"}`)}
          onChange={(v) => set("mode", v as PageTheme["mode"])}
        />

        <Field label={t("colors")} hint={t("colorsHint")}>
          <ColorTable theme={th} onColor={setColor} />
          <div className="mt-2 flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() =>
                onChange({
                  ...th,
                  light: { ...(th.light ?? {}), ...colorBlindStatus.light },
                  dark: { ...(th.dark ?? {}), ...colorBlindStatus.dark },
                })
              }
            >
              {t("colorBlind")}
            </Button>
            <Button type="button" variant="ghost" size="xs" onClick={() => onChange({ ...th, light: {}, dark: {} })}>
              <RotateCcwIcon /> {t("colorsReset")}
            </Button>
          </div>
        </Field>

        <div className="grid gap-5 sm:grid-cols-2">
          <Choice
            label={t("font")}
            value={th.font ?? ""}
            options={["", "plex", "figtree", "atkinson", "serif", "system"]}
            labels={(v) => (v === "system" ? t("fontSystem") : fontNames[v as keyof typeof fontNames])}
            onChange={(v) => set("font", v as PageTheme["font"])}
          />
          <Choice
            label={t("size")}
            value={th.size ?? ""}
            options={["s", "", "l"]}
            labels={(v) => tk(`sizes.${v || "m"}`)}
            onChange={(v) => set("size", v as PageTheme["size"])}
          />
          <Choice
            label={t("radius")}
            value={th.radius === undefined || th.radius === null ? "" : String(th.radius)}
            options={["", ...radii.map(String)]}
            labels={(v) => (v === "" ? t("default") : `${v}px`)}
            onChange={(v) => set("radius", v === "" ? undefined : Number(v))}
          />
          <Choice
            label={t("style")}
            value={th.style ?? ""}
            options={["", "solid", "cards"]}
            labels={(v) => tk(`styles.${v || "dashed"}`)}
            onChange={(v) => set("style", v as PageTheme["style"])}
          />
          <Choice
            label={t("density")}
            value={th.density ?? ""}
            options={["compact", "", "airy"]}
            labels={(v) => tk(`densities.${v || "normal"}`)}
            onChange={(v) => set("density", v as PageTheme["density"])}
          />
          <Choice
            label={t("width")}
            value={th.width ?? ""}
            options={["", "wide"]}
            labels={(v) => tk(`widths.${v || "narrow"}`)}
            onChange={(v) => set("width", v as PageTheme["width"])}
          />
          <Choice
            label={t("bars")}
            value={th.bars ?? ""}
            options={["", "round", "thin"]}
            labels={(v) => tk(`barStyles.${v || "blocks"}`)}
            onChange={(v) => set("bars", v as PageTheme["bars"])}
          />
        </div>

        <div className="grid gap-5 sm:grid-cols-2">
          <Choice
            label={t("headerStyle")}
            value={th.header_style ?? ""}
            options={["", "band"]}
            labels={(v) => tk(`headerStyles.${v || "plain"}`)}
            onChange={(v) => set("header_style", v as PageTheme["header_style"])}
          />
          <Choice
            label={t("headerAlign")}
            value={th.header_align ?? ""}
            options={["", "center"]}
            labels={(v) => tk(`aligns.${v || "left"}`)}
            onChange={(v) => set("header_align", v as PageTheme["header_align"])}
          />
          <Field label={t("tagline")} htmlFor="th-tagline" className="sm:col-span-2" hint={t("taglineHint")}>
            <Input id="th-tagline" maxLength={200} value={th.tagline ?? ""} onChange={(e) => set("tagline", e.target.value)} />
          </Field>
          <Field label={t("headerLinks")} className="sm:col-span-2" hint={t("linksHint")}>
            <LinksEditor links={th.header_links ?? []} max={6} onChange={(l) => set("header_links", l)} />
          </Field>
          <Field label={t("notice")} htmlFor="th-notice" className="sm:col-span-2" hint={t("markdownHint")}>
            <Textarea
              id="th-notice"
              rows={2}
              maxLength={500}
              value={th.notice ?? ""}
              placeholder={t("noticePlaceholder")}
              onChange={(e) => set("notice", e.target.value)}
            />
          </Field>
          <Field label={t("footerText")} htmlFor="th-footer" className="sm:col-span-2" hint={t("markdownHint")}>
            <Textarea
              id="th-footer"
              rows={3}
              maxLength={2000}
              value={th.footer_text ?? ""}
              placeholder={t("footerPlaceholder")}
              onChange={(e) => set("footer_text", e.target.value)}
            />
          </Field>
          <Field label={t("footerLinks")} className="sm:col-span-2" hint={t("linksHint")}>
            <LinksEditor links={th.footer_links ?? []} max={12} onChange={(l) => set("footer_links", l)} />
          </Field>
          <label className="flex items-center gap-2.5">
            <Switch checked={!!th.hide_powered_by} onCheckedChange={(v) => set("hide_powered_by", v)} />
            <span>{t("hidePoweredBy")}</span>
          </label>
          <label className={cn("flex items-center gap-2.5", !hasLogo && "opacity-60")}>
            <Switch checked={!!th.logo_favicon} disabled={!hasLogo && !th.logo_favicon} onCheckedChange={(v) => set("logo_favicon", v)} />
            <span>
              {t("logoFavicon")}
              {!hasLogo && <span className="block text-[11px] text-muted-foreground">{t("logoFaviconNoLogo")}</span>}
            </span>
          </label>
        </div>

        <Field label={t("css")} htmlFor="th-css" hint={t("cssHint")}>
          <Textarea
            id="th-css"
            rows={6}
            spellCheck={false}
            maxLength={32768}
            className="font-mono text-xs md:text-xs"
            value={th.css ?? ""}
            placeholder={".pt-banner {\n  font-size: 1.1em;\n}"}
            onChange={(e) => set("css", e.target.value)}
          />
        </Field>
      </div>
    </Section>
  )
}

function Choice({
  label,
  value,
  options,
  labels,
  onChange,
}: {
  label: string
  value: string
  options: string[]
  labels: (v: string) => string
  onChange: (v: string) => void
}) {
  return (
    <Field label={label}>
      <div className="flex flex-wrap gap-1" role="radiogroup" aria-label={label}>
        {options.map((o) => (
          <button
            key={o}
            type="button"
            role="radio"
            aria-checked={value === o}
            className={cn(segment({ active: value === o }), "border border-dashed", value === o && "border-solid")}
            onClick={() => onChange(o)}
          >
            {labels(o)}
          </button>
        ))}
      </div>
    </Field>
  )
}

/** One row per color, a picker for light and one for dark, with contrast checks. */
function ColorTable({ theme, onColor }: { theme: PageTheme; onColor: (m: Mode, k: ColorKey, v: string) => void }) {
  const t = useTranslations("statusPageTheme")
  // Keys built from option values ("modes.dark", "styles.cards"...).
  const tk = t as unknown as (key: string) => string
  const modes: Mode[] = ["light", "dark"]
  const forced = theme.mode || ""
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr className="text-left text-muted-foreground">
            <th className="py-1 pr-3 font-normal">{t("color")}</th>
            {modes.map((m) => (
              <th key={m} className={cn("py-1 pr-3 font-normal", forced && forced !== m && "opacity-40")}>
                {tk(`modes.${m}`)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {colorKeys.map((k) => (
            <tr key={k} className="border-t border-dashed">
              <td className="py-1.5 pr-3">{tk(`colorNames.${k}`)}</td>
              {modes.map((m) => {
                const own = theme[m]?.[k] ?? ""
                const shown = own || defaultColors[m][k]
                return (
                  <td key={m} className={cn("py-1.5 pr-3", forced && forced !== m && "opacity-40")}>
                    <div className="flex items-center gap-1.5">
                      <input
                        type="color"
                        aria-label={`${tk(`colorNames.${k}`)} · ${tk(`modes.${m}`)}`}
                        value={shown}
                        onChange={(e) => onColor(m, k, e.target.value)}
                        className="h-6 w-8 cursor-pointer border bg-transparent p-0.5"
                      />
                      <span className={cn("tabular-nums", !own && "text-muted-foreground")}>{own || t("default")}</span>
                      {own && (
                        <button
                          type="button"
                          className="text-muted-foreground hover:text-foreground"
                          aria-label={t("colorReset")}
                          onClick={() => onColor(m, k, "")}
                        >
                          <XIcon className="size-3" />
                        </button>
                      )}
                    </div>
                  </td>
                )
              })}
            </tr>
          ))}
          <tr className="border-t border-dashed align-top">
            <td className="py-1.5 pr-3 text-muted-foreground">{t("contrast")}</td>
            {modes.map((m) => (
              <td key={m} className={cn("py-1.5 pr-3", forced && forced !== m && "opacity-40")}>
                <ContrastList theme={theme} mode={m} />
              </td>
            ))}
          </tr>
        </tbody>
      </table>
    </div>
  )
}

/** WCAG contrast of the colors that carry text, against the background. */
function ContrastList({ theme, mode }: { theme: PageTheme; mode: Mode }) {
  const t = useTranslations("statusPageTheme")
  const c = resolvedColors(theme, mode)
  const checks: [string, number][] = [
    [t("colorNames.text"), contrast(c.text, c.background)],
    [t("colorNames.muted"), contrast(c.muted, c.background)],
    [t("colorNames.accent"), contrast(c.accent, c.background)],
    [t("colorNames.down"), contrast(c.down, c.background)],
  ]
  return (
    <ul className="grid gap-0.5">
      {checks.map(([name, r]) => (
        <li
          key={name}
          className={cn("tabular-nums", r >= 4.5 ? "text-up" : r >= 3 ? "text-warn" : "text-down")}
          title={r >= 4.5 ? "WCAG AA" : t("lowContrast")}
        >
          {name} {r.toFixed(1)}:1 {r >= 4.5 ? "AA" : r >= 3 ? t("contrastLarge") : t("contrastLow")}
        </li>
      ))}
    </ul>
  )
}

function LinksEditor({ links, max, onChange }: { links: PageLink[]; max: number; onChange: (l: PageLink[]) => void }) {
  const t = useTranslations("statusPageTheme")
  const update = (i: number, l: Partial<PageLink>) => onChange(links.map((x, j) => (j === i ? { ...x, ...l } : x)))
  return (
    <div className="grid gap-2">
      {links.map((l, i) => (
        <div key={i} className="flex gap-2">
          <Input
            aria-label={t("linkLabel")}
            placeholder={t("linkLabel")}
            maxLength={60}
            value={l.label}
            onChange={(e) => update(i, { label: e.target.value })}
            className="w-40 shrink-0"
          />
          <Input
            aria-label={t("linkUrl")}
            placeholder="https://example.com"
            inputMode="url"
            maxLength={2048}
            value={l.url}
            onChange={(e) => update(i, { url: e.target.value })}
            onKeyDown={(e) => e.key === "Enter" && e.preventDefault()}
          />
          <Button type="button" variant="ghost" size="icon" aria-label={t("linkRemove")} onClick={() => onChange(links.filter((_, j) => j !== i))}>
            <XIcon />
          </Button>
        </div>
      ))}
      {links.length < max && (
        <div>
          <Button type="button" variant="outline" size="xs" onClick={() => onChange([...links, { label: "", url: "" }])}>
            <PlusIcon /> {t("linkAdd")}
          </Button>
        </div>
      )}
    </div>
  )
}
