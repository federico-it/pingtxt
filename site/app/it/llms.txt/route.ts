import { llmsTxt, TEXT } from "@/lib/llms"

export const dynamic = "force-static"

// pingtxt.com/it/llms.txt, see lib/llms.ts.
export function GET() {
  return new Response(llmsTxt("it"), { headers: TEXT })
}
