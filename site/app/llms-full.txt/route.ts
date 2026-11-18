import { llmsFullTxt, TEXT } from "@/lib/llms"

export const dynamic = "force-static"

// pingtxt.com/llms-full.txt, see lib/llms.ts.
export function GET() {
  return new Response(llmsFullTxt("en"), { headers: TEXT })
}
