"use client"

import { useState } from "react"
import { useTranslations } from "use-intl"

import { useApi, type VersionStatus } from "@/lib/api"

const DISMISS_KEY = "pingtxt.update-dismissed"
export const UPGRADE_DOCS = "https://pingtxt.com/docs/#upgrade"

/** Version status for admins (null for read-only users: they cannot update). */
export function useVersionStatus(isAdmin: boolean) {
  return useApi<VersionStatus>(isAdmin ? "/api/version" : null, {
    refreshInterval: 60 * 60_000,
  })
}

/**
 * A thin bar under the header when a newer release is out. Dismissing it
 * hides it until the next version; the footer keeps a small link.
 */
export function UpdateNotice({ status }: { status: VersionStatus | undefined }) {
  const t = useTranslations("update")
  // The shell renders this only in the browser, after the user is loaded.
  const [dismissed, setDismissed] = useState<string | null>(() => {
    try {
      return localStorage.getItem(DISMISS_KEY)
    } catch {
      return null // storage blocked: the notice just shows again
    }
  })

  const latest = status?.update_available ? status.latest : null
  if (!latest || dismissed === latest.version) return null

  function dismiss() {
    if (!latest) return
    setDismissed(latest.version)
    try {
      localStorage.setItem(DISMISS_KEY, latest.version)
    } catch {}
  }

  return (
    <div className="-mx-4 flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-dashed bg-info/5 px-4 py-2 text-xs sm:-mx-6 sm:px-6">
      <span>
        <span className="text-info">*</span>{" "}
        {t.rich("available", {
          latest: latest.version,
          current: status?.current ?? "",
          b: (c) => <b className="text-foreground">{c}</b>,
        })}
      </span>
      <a
        href={latest.url}
        target="_blank"
        rel="noreferrer"
        className="text-muted-foreground underline underline-offset-4 hover:text-foreground"
      >
        {t("notes")}
      </a>
      <a
        href={UPGRADE_DOCS}
        target="_blank"
        rel="noreferrer"
        className="text-muted-foreground underline underline-offset-4 hover:text-foreground"
      >
        {t("how")}
      </a>
      <button
        type="button"
        onClick={dismiss}
        className="ml-auto text-muted-foreground hover:text-foreground"
        aria-label={t("dismiss")}
        title={t("dismiss")}
      >
        [x]
      </button>
    </div>
  )
}
