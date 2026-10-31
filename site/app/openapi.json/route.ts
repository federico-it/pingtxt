import { rawOpenAPI } from "@/lib/openapi"

export const dynamic = "force-static"

// pingtxt.com/openapi.json: the API document of the latest version on main.
export function GET() {
  return new Response(rawOpenAPI(), { headers: { "Content-Type": "application/json" } })
}
