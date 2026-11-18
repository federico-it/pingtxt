import { docMarkdown, MARKDOWN } from "@/lib/llms"

export const dynamic = "force-static"

// The docs index as Markdown, see lib/llms.ts.
export function GET() {
  return new Response(docMarkdown("it", ""), { headers: MARKDOWN })
}
