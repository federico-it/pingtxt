import { checkDocs, DOC_PAGES, type DocSlug } from "@/lib/docs"
import { docMarkdown, MARKDOWN } from "@/lib/llms"

export const dynamic = "force-static"
export const dynamicParams = false

export function generateStaticParams() {
  return DOC_PAGES.map((slug) => ({ slug }))
}

// One docs page as Markdown, see lib/llms.ts.
export async function GET(_: Request, { params }: { params: Promise<{ slug: string }> }) {
  checkDocs()
  return new Response(docMarkdown("en", (await params).slug as DocSlug), { headers: MARKDOWN })
}
