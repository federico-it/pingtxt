import { apiMarkdown, MARKDOWN } from "@/lib/llms"

export const dynamic = "force-static"

// The REST API endpoints as Markdown, see lib/llms.ts.
export function GET() {
  return new Response(apiMarkdown("it"), { headers: MARKDOWN })
}
