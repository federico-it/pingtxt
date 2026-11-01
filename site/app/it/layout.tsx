import { Root, rootMetadata } from "@/components/root"

export { viewport } from "@/components/root"
export const metadata = rootMetadata("it")

export default function Layout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <Root lang="it">{children}</Root>
}
