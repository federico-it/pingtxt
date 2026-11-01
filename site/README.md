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

## SEO

- `lib/seo.ts`: `pageMeta()` gives each page its title, description, canonical
  URL and Open Graph/Twitter card. Use it in every new page.
- `app/sitemap.ts` lists the pages for `sitemap.xml`: add new pages there.
  `app/robots.ts` builds `robots.txt`.
- The home page has schema.org JSON-LD (`SoftwareApplication`).
- Screenshots (`components/shot.tsx`) need a descriptive `alt` and their
  `width`/`height`; `label` is only the fake command in the title bar.
