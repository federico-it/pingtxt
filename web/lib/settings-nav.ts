import { useApi, type Me } from "@/lib/api"

/**
 * Settings categories, in sidebar order. Each one is a page at
 * /settings/<slug>/. Admin categories are hidden from read-only users.
 */
export const settingsCategories = [
  { slug: "profile", admin: false },
  { slug: "account", admin: false },
  { slug: "tokens", admin: false },
  { slug: "general", admin: true },
  { slug: "monitors", admin: true },
  { slug: "access", admin: true },
  { slug: "integrations", admin: true },
  { slug: "data", admin: true },
] as const

export type SettingsCategory = (typeof settingsCategories)[number]["slug"]

export const settingsHref = (slug: SettingsCategory) => `/settings/${slug}/`

/**
 * Where old one-page links (/settings#mobile-app) land now. The hash is kept
 * so the page can scroll to that section.
 */
export const legacyAnchors: Record<string, SettingsCategory> = {
  "mobile-app": "integrations",
}

/** Categories the current user can open, grouped as in the sidebar. */
export function useSettingsGroups() {
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 0 })
  const isAdmin = me.data?.user.role === "admin"
  const pick = (admin: boolean): SettingsCategory[] =>
    settingsCategories.filter((c) => c.admin === admin).map((c) => c.slug)
  return [
    { key: "personal" as const, cats: pick(false) },
    ...(isAdmin ? [{ key: "instance" as const, cats: pick(true) }] : []),
  ]
}
