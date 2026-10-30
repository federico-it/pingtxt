import type { NextConfig } from "next"

const isDev = process.env.NODE_ENV !== "production"
const hub = process.env.PINGTXT_DEV_HUB ?? "http://127.0.0.1:8090"

// Production: static export embedded and served by the Go hub.
// Development: `next dev` proxies API calls to a locally running hub.
const nextConfig: NextConfig = isDev
  ? {
      trailingSlash: true,
      async rewrites() {
        return [
          { source: "/api/:path*", destination: `${hub}/api/:path*` },
          { source: "/install-agent.sh", destination: `${hub}/install-agent.sh` },
        ]
      },
    }
  : {
      output: "export",
      trailingSlash: true,
      images: { unoptimized: true },
    }

export default nextConfig
