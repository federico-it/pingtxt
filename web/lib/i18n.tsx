"use client"

import { Fragment, createContext, useCallback, useContext, useEffect, useState } from "react"
import { IntlProvider, createTranslator, type AbstractIntlMessages } from "use-intl"

import en from "@/messages/en.json"

/**
 * Languages offered in the picker. English is the source language and is
 * bundled; the others are loaded on demand from web/messages/<code>.json:
 * add a locale here once its file is reasonably complete.
 */
export const locales = {
  en: "English",
  it: "Italiano",
} as const

export type Locale = keyof typeof locales

const loaders: Record<Locale, () => Promise<AbstractIntlMessages>> = {
  en: async () => en,
  it: () => import("@/messages/it.json").then((m) => m.default),
}

const STORAGE_KEY = "pingtxt.locale"

export function isLocale(v: unknown): v is Locale {
  return typeof v === "string" && Object.hasOwn(locales, v)
}

/** Saved choice first, then the browser languages, then English. */
function detectLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isLocale(saved)) return saved
  } catch {}
  for (const lang of navigator.languages ?? [navigator.language]) {
    if (isLocale(lang)) return lang
    const base = lang.split("-")[0]
    if (isLocale(base)) return base
  }
  return "en"
}

/** Missing keys in a translation fall back to the English text. */
function withFallback(base: AbstractIntlMessages, over: AbstractIntlMessages): AbstractIntlMessages {
  const out: AbstractIntlMessages = { ...base }
  for (const [k, v] of Object.entries(over)) {
    const b = base[k]
    if (typeof v === "string") {
      if (v !== "") out[k] = v
    } else if (v && typeof b === "object") {
      out[k] = withFallback(b, v)
    }
  }
  return out
}

async function load(locale: Locale): Promise<{ locale: Locale; messages: AbstractIntlMessages }> {
  if (locale === "en") return { locale, messages: en }
  try {
    return { locale, messages: withFallback(en, await loaders[locale]()) }
  } catch {
    return { locale: "en", messages: en }
  }
}

let translator = createTranslator({ locale: "en", messages: en })

/**
 * Translates outside React components (formatting helpers). Components get
 * new text after a language change because the provider remounts the tree.
 */
export function tr(key: Parameters<typeof translator>[0], values?: Parameters<typeof translator>[1]): string {
  return translator(key, values)
}

const LocaleContext = createContext<{ locale: Locale; setLocale: (l: Locale) => void }>({
  locale: "en",
  setLocale: () => {},
})

export function useLocale() {
  return useContext(LocaleContext)
}

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<{ locale: Locale; messages: AbstractIntlMessages }>({
    locale: "en",
    messages: en,
  })

  const apply = useCallback((locale: Locale) => {
    void load(locale).then((next) => {
      translator = createTranslator({ locale: next.locale, messages: next.messages as typeof en })
      document.documentElement.lang = next.locale
      setState(next)
    })
  }, [])

  useEffect(() => {
    const l = detectLocale()
    if (l !== "en") apply(l)
  }, [apply])

  const setLocale = useCallback(
    (l: Locale) => {
      try {
        localStorage.setItem(STORAGE_KEY, l)
      } catch {}
      apply(l)
    },
    [apply]
  )

  return (
    <LocaleContext.Provider value={{ locale: state.locale, setLocale }}>
      <IntlProvider
        locale={state.locale}
        messages={state.messages}
        onError={(e) => {
          if (process.env.NODE_ENV !== "production") console.warn(e.message)
        }}
      >
        {/* The key remounts the app so text computed outside hooks is redone too. */}
        <Fragment key={state.locale}>{children}</Fragment>
      </IntlProvider>
    </LocaleContext.Provider>
  )
}
