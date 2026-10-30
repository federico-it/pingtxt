import type { NextConfig } from "next"

// Static export for GitHub Pages (pingtxt.com, served from the domain root).
const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
}

export default nextConfig
