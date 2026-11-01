import { llmsFullTxt } from "@/lib/llms"

export const dynamic = "force-static"

// pingtxt.com/llms-full.txt, see lib/llms.ts.
export function GET() {
  return new Response(llmsFullTxt(), { headers: { "Content-Type": "text/plain; charset=utf-8" } })
}
