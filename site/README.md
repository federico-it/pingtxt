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
