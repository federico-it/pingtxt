import { searchIndex } from "@/lib/docs"

export const dynamic = "force-static"

// The docs search index in English (see searchIndex).
export function GET() {
  return Response.json(searchIndex("en"))
}
