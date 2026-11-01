import { llmsTxt } from "@/lib/llms"

export const dynamic = "force-static"

// pingtxt.com/llms.txt, see lib/llms.ts.
export function GET() {
  return new Response(llmsTxt(), { headers: { "Content-Type": "text/plain; charset=utf-8" } })
}
