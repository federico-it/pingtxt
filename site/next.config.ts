import type { NextConfig } from "next"

// Static export for GitHub Pages (pingtxt.com, served from the domain root).
// English pages are in app/(en), Italian ones in app/it: two root layouts, so
// the 404 page is app/global-not-found.tsx.
const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
  experimental: { globalNotFound: true },
}

export default nextConfig
