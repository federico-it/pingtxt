"use client"

import "swagger-ui-dist/swagger-ui.css"
import "./swagger.css"

import { useEffect, useRef, useState } from "react"
import { useTranslations } from "use-intl"

import { PageHeader } from "@/components/page-header"

// Interactive API reference: Swagger UI on top of /api/openapi.json.
// Swagger UI is bundled with the app (no CDN). "Try it out" sends requests
// with the browser session, or with a token set through "Authorize".
export default function ApiDocsPage() {
  const t = useTranslations("apiDocs")
  const ref = useRef<HTMLDivElement>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [{ SwaggerUIBundle }, res] = await Promise.all([
          import("swagger-ui-dist"),
          fetch("/api/openapi.json"),
        ])
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        const spec = await res.json()
        // Send requests to the address in the address bar, even when the
        // configured public URL is another one.
        spec.servers = [
          { url: window.location.origin, description: "this hub" },
        ]
        if (cancelled || !ref.current) return
        SwaggerUIBundle({
          spec,
          domNode: ref.current,
          deepLinking: true,
          persistAuthorization: true,
          docExpansion: "list",
          defaultModelsExpandDepth: 0,
          tryItOutEnabled: false,
          displayRequestDuration: true,
          filter: true,
        })
      } catch (err) {
        if (!cancelled)
          setError(err instanceof Error ? err.message : String(err))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <>
      <PageHeader cmd="curl /api/openapi.json" title={t("title")}>
        <a
          href="/api/openapi.json"
          target="_blank"
          rel="noreferrer"
          className="text-xs text-muted-foreground underline-offset-4 hover:underline"
        >
          openapi.json
        </a>
      </PageHeader>
      <p className="mb-4 max-w-3xl text-xs text-muted-foreground">
        {t("intro")}
      </p>
      {error && <p className="text-xs text-down">{t("error", { error })}</p>}
      <div ref={ref} className="pingtxt-swagger" />
    </>
  )
}
