import Link from "next/link"

export default function NotFound() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-2">
      <div className="text-xs text-muted-foreground">
        <span className="text-primary">$</span> cd {"<path>"}
      </div>
      <div>404: no such file or directory</div>
      <Link href="/" className="text-primary underline underline-offset-4">
        cd ~
      </Link>
    </div>
  )
}
