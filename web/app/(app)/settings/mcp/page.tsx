"use client"

import Link from "next/link"
import { useTranslations } from "use-intl"

import { CopyBlock } from "@/components/copy-block"
import { Section } from "@/components/page-header"
import { ConnectedApps } from "@/components/settings/connected-apps"
import { useIsAdmin } from "@/lib/api"
import { settingsHref } from "@/lib/settings-nav"

const MCP_DOCS = "https://pingtxt.com/docs/integrations/#mcp-server"

const link = "text-foreground underline underline-offset-4"

/** The MCP server: its address, how assistants sign in, which ones are connected. */
export default function McpSettings() {
  const t = useTranslations("mcpSettings")
  const isAdmin = useIsAdmin()
  const origin = typeof window === "undefined" ? "" : window.location.origin
  const url = origin + "/mcp"

  return (
    <>
      <Section title={t("server")}>
        <div className="grid gap-3">
          <p className="text-xs text-muted-foreground">{t("intro")}</p>
          <CopyBlock code={url} />
          <p className="text-xs text-muted-foreground">
            {isAdmin === false ? t("accessViewer") : t("access")}{" "}
            {t.rich("docs", {
              docs: (c) => (
                <a href={MCP_DOCS} target="_blank" rel="noreferrer" className={link}>
                  {c}
                </a>
              ),
            })}
          </p>
        </div>
      </Section>

      <Section title={t("oauth")}>
        <div className="grid gap-3">
          <p className="text-xs text-muted-foreground">{t("oauthIntro")}</p>
          <p className="text-xs">
            <span className="text-foreground">claude.ai, Claude Desktop</span>{" "}
            <span className="text-muted-foreground">{t("oauthClaudeAi")}</span>
          </p>
          <p className="text-xs">
            <span className="text-foreground">Claude Code</span>{" "}
            <span className="text-muted-foreground">{t("oauthClaudeCode")}</span>
          </p>
          <CopyBlock code={`claude mcp add --transport http pingtxt ${url}`} />
        </div>
      </Section>

      <Section title={t("token")}>
        <div className="grid gap-3">
          <p className="text-xs text-muted-foreground">
            {t.rich("tokenIntro", {
              tokens: (c) => (
                <Link href={settingsHref("tokens")} className={link}>
                  {c}
                </Link>
              ),
            })}
          </p>
          <CopyBlock
            code={`{ "mcpServers": { "pingtxt": { "url": "${url}", "headers": { "Authorization": "Bearer ptk_..." } } } }`}
          />
        </div>
      </Section>

      <Section title={t("connected")}>
        <div className="grid gap-3">
          <p className="text-xs text-muted-foreground">{t("connectedIntro")}</p>
          <ConnectedApps
            kind="assistants"
            empty={
              <p className="border border-dashed px-3 py-2 text-xs text-muted-foreground">
                {t("none")}
              </p>
            }
          />
        </div>
      </Section>
    </>
  )
}
