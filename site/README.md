# pingtxt.com

Public website: a Next.js static export deployed to GitHub Pages by
`.github/workflows/pages.yml` on every push to `main` that touches `site/`.

```sh
cd site
npm install
npm run dev      # http://localhost:3000
npm run build    # static files in site/out
```

`public/CNAME` sets the custom domain. Screenshots live in `public/screenshots/`.

## Languages

The site is in English (`/`) and Italian (`/it/`). English pages are in
`app/(en)`, Italian ones in `app/it`: two root layouts, so `<html lang>` is
right, and the 404 page is `app/global-not-found.tsx` (in both languages).
Each route is a few lines that render the shared page in `content/` with its
language.

- Texts sit side by side in `content/*.tsx` and the components:
  `t("english", "italiano")` from `tx(lang)` (server) or `useT()` (client).
  When you change a text, change both. Italian is written here by hand, not
  on Crowdin: Crowdin translates the hub UI (`web/messages`), whose strings
  are short keys; the site is long text with code and links inside.
- Internal links go through `localePath(lang, "/docs/")`. Section ids
  (`#install`, `#usage-stats`, ...) are the same in both languages.
- The header has a `[it]` / `[en]` switch to the same page; on English pages a
  browser set to Italian gets a one-line hint. The choice is kept in
  `localStorage` (`pingtxt.lang`); there is no redirect.
- API reference: the page text is translated, endpoint descriptions come from
  the OpenAPI document and stay in English.

## SEO

- `lib/seo.ts`: `pageMeta()` gives each page its title, description, canonical
  URL, hreflang alternates (en, it, x-default) and Open Graph/Twitter card.
  Use it in every new page.
- `app/sitemap.ts` lists the pages for `sitemap.xml` (English path; each is
  listed once per language with hreflang links): add new pages there.
  `app/robots.ts` builds `robots.txt`.
- The home page has schema.org JSON-LD (`SoftwareApplication`).
- Screenshots (`components/shot.tsx`) need a descriptive `alt` and their
  `width`/`height`; `label` is only the fake command in the title bar.
- `llms.txt` (map of the site for AI assistants, `lib/llms.ts`) and
  `llms-full.txt` (the repository README, read at build time). New docs
  sections go in the list in `lib/llms.ts` too.
