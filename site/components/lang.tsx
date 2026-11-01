"use client"

import { createContext, useContext } from "react"

import { tx, type Lang } from "@/lib/i18n"

const LangContext = createContext<Lang>("en")

export function LangProvider({ lang, children }: { lang: Lang; children: React.ReactNode }) {
  return <LangContext.Provider value={lang}>{children}</LangContext.Provider>
}

export function useLang(): Lang {
  return useContext(LangContext)
}

// useT() is tx() for client components: t("english", "italiano").
export function useT() {
  return tx(useLang())
}

// Where the visitor's choice of language is kept (only in their browser).
export const LANG_KEY = "pingtxt.lang"
