/** ISO 3166-1 alpha-2 codes of the countries an agent can be in. */
export const COUNTRY_CODES = (
  "AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ " +
  "CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR " +
  "GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP " +
  "KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS " +
  "MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS " +
  "RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW " +
  "TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS XK YE YT ZA ZM ZW"
).split(" ")

/** Flag emoji of a two-letter country code ("" for anything else). */
export function countryFlag(code?: string): string {
  if (!code || !/^[A-Za-z]{2}$/.test(code)) return ""
  const up = code.toUpperCase()
  return String.fromCodePoint(...[...up].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}

const names = new Map<string, Intl.DisplayNames | null>()

/** Name of a country in the given UI language, the code when unknown. */
export function countryName(code: string, locale: string): string {
  if (!names.has(locale)) {
    try {
      names.set(locale, new Intl.DisplayNames([locale], { type: "region" }))
    } catch {
      names.set(locale, null)
    }
  }
  try {
    return names.get(locale)?.of(code.toUpperCase()) ?? code
  } catch {
    return code
  }
}

/** Every country as [code, name], sorted by name in the given language. */
export function countryOptions(locale: string): [string, string][] {
  const out = COUNTRY_CODES.map((c): [string, string] => [c, countryName(c, locale)])
  const coll = new Intl.Collator(locale)
  return out.sort((a, b) => coll.compare(a[1], b[1]))
}
