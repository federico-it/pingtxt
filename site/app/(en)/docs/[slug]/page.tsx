import { DocsPage, docsMeta } from "@/content/docs"
import { DOC_PAGES, type DocSlug } from "@/lib/docs"

type Props = { params: Promise<{ slug: DocSlug }> }

export const dynamicParams = false

export function generateStaticParams() {
  return DOC_PAGES.map((slug) => ({ slug }))
}

export async function generateMetadata({ params }: Props) {
  return docsMeta("en", (await params).slug)
}

export default async function Page({ params }: Props) {
  return <DocsPage lang="en" slug={(await params).slug} />
}
