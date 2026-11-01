// The site is in English (at /) and Italian (at /it/). Texts live next to
// each other in the page components: t("english", "italiano"). Italian is
// written by hand in this repository, not on Crowdin (that is for the hub UI).

export const LANGS = ["en", "it"] as const
export type Lang = (typeof LANGS)[number]

export const LANG_NAME: Record<Lang, string> = { en: "english", it: "italiano" }
export const OG_LOCALE: Record<Lang, string> = { en: "en_US", it: "it_IT" }

// localePath("it", "/docs/") is "/it/docs/". `path` starts with a slash and,
// for pages, ends with one (trailingSlash); a "#hash" may follow.
export function localePath(lang: Lang, path: string) {
  return lang === "en" ? path : `/${lang}${path}`
}

// splitPath("/it/docs/") is ["it", "/docs/"].
export function splitPath(pathname: string): [Lang, string] {
  const p = pathname.endsWith("/") ? pathname : `${pathname}/`
  for (const l of LANGS) {
    if (l !== "en" && (p === `/${l}/` || p.startsWith(`/${l}/`))) return [l, p.slice(l.length + 1)]
  }
  return ["en", p]
}

// tx(lang) returns t(en, it), which picks the text (or JSX) of the language.
export function tx(lang: Lang) {
  return function t<T>(en: T, it: T): T {
    return lang === "it" ? it : en
  }
}
