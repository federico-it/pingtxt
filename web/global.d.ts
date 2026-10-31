import type en from "./messages/en.json"
import type { Locale } from "./lib/i18n"

declare module "use-intl" {
  interface AppConfig {
    Locale: Locale
    Messages: typeof en
  }
}
