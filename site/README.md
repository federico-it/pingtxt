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

- The docs pages (`/docs/<page>/`) are not written here: they render the
  Markdown files in the repository's `docs/en` and `docs/it` folders at build
  time (`lib/docs.ts`, `content/docs.tsx`). The build fails when a page is
  missing in a language, when the two languages have different headings, or
  when a link points to a page or anchor that does not exist. Old links to the
  single docs page (`/docs/#upgrade`) are sent to the new page by a script on
  `/docs/` (`OLD_ANCHORS`). Use `docHref(lang, page, englishAnchor)` to link a
  docs section from a page here.
- Texts sit side by side in `content/*.tsx` and the components:
  `t("english", "italiano")` from `tx(lang)` (server) or `useT()` (client).
  When you change a text, change both. The site does not use the hub UI's
  message files (`web/messages`): it is long text with code and links inside.
- Internal links go through `localePath(lang, "/docs/")`. Section ids of
  the home page (`#install`, `#features`) are the same in both languages.
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
- For AI assistants (`lib/llms.ts`), per language (`/` and `/it/`):
  `llms.txt` (every docs page and section, with how to read them),
  `llms-full.txt` (every docs page in one file) and the Markdown of each page
  at `docs/<page>.md` (`docs/<page>/index.html.md` too, copied by
  `scripts/md-alias.mjs` after `next build`), with links to other `.md`
  pages; `docs/api.md` lists the REST API. Docs pages link their Markdown as
  `<link rel="alternate" type="text/markdown">`. All built from `docs/`, so
  new pages show up by themselves.
