import type { Metadata } from "next";

import { CopyBlock } from "@/components/copy-block";
import { Prompt, REPO, SectionTitle } from "@/components/site-shell";
import {
  loadOpenAPI,
  mediaExample,
  resolve,
  typeName,
  type Access,
  type OpenAPI,
  type Operation,
  type Schema,
} from "@/lib/openapi";
import { tx, type Lang } from "@/lib/i18n";
import { pageMeta } from "@/lib/seo";

// The page text is t(english, italiano). Endpoint summaries and
// descriptions come from the OpenAPI document and stay in English.
export function apiMeta(lang: Lang): Metadata {
  const t = tx(lang);
  return pageMeta({
    lang,
    title: t("REST API reference", "riferimento della REST API"),
    description: t(
      "Every endpoint of the pingtxt REST API: authentication with API tokens, roles, parameters and examples.",
      "Tutti gli endpoint della REST API di pingtxt: autenticazione con token API, ruoli, parametri ed esempi.",
    ),
    path: "/docs/api/",
  });
}

const methodColor: Record<string, string> = {
  get: "text-up",
  post: "text-blue-500",
  put: "text-amber-500",
  delete: "text-down",
};

const accessLabel: Record<Access, [string, string]> = {
  public: ["public", "pubblico"],
  read: ["read token", "token di lettura"],
  write: ["write token · admin", "token di scrittura · admin"],
  session: ["browser session", "sessione del browser"],
  admin: ["admin session", "sessione admin"],
};

const accessHelp: [Access, string, string][] = [
  ["public", "no authentication", "nessuna autenticazione"],
  [
    "read",
    "any user or API token, a read token is enough",
    "qualsiasi utente o token API, basta un token di lettura",
  ],
  [
    "write",
    "an admin, with a write token or the browser session; viewers and read tokens get 403",
    "un admin, con un token di scrittura o la sessione del browser; utenti in sola lettura e token di lettura ricevono 403",
  ],
  [
    "session",
    "browser session only, API tokens get 403",
    "solo sessione del browser, i token API ricevono 403",
  ],
  [
    "admin",
    "browser session of an admin only",
    "solo la sessione del browser di un admin",
  ],
];

const link = "text-foreground underline underline-offset-4";

function C({ children }: { children: React.ReactNode }) {
  return <code className="bg-muted px-1 text-foreground">{children}</code>;
}

function slug(s: string) {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, "-");
}

// Inline `code` in descriptions.
function Text({ children }: { children?: string }) {
  if (!children) return null;
  const parts = children.split(/(`[^`]+`|\*\*[^*]+\*\*)/g);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("`") ? (
          <code key={i} className="bg-muted px-1 text-foreground">
            {p.slice(1, -1)}
          </code>
        ) : p.startsWith("**") ? (
          <b key={i} className="text-foreground">
            {p.slice(2, -2)}
          </b>
        ) : (
          p
        ),
      )}
    </>
  );
}

function Values({ lang, values }: { lang: Lang; values: unknown[] }) {
  return (
    <span className="block">
      {tx(lang)("one of:", "uno tra:")}{" "}
      {values.map((v, i) => (
        <span key={i}>
          {i > 0 && " "}
          <code className="bg-muted px-1 text-foreground">
            {v === "" ? '""' : String(v)}
          </code>
        </span>
      ))}
    </span>
  );
}

function Fields({
  lang,
  doc,
  schema,
}: {
  lang: Lang;
  doc: OpenAPI;
  schema: Schema;
}) {
  const s = resolve(doc, schema);
  const parts = s.allOf ? s.allOf.map((x) => resolve(doc, x)) : [s];
  const props = parts.flatMap((p) =>
    Object.entries(p.properties ?? {}).map(
      ([k, v]) => [k, v, p.required?.includes(k)] as const,
    ),
  );
  if (props.length === 0) return null;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-[12px]">
        <tbody>
          {props.map(([k, v, req]) => (
            <tr key={k} className="border-b border-dashed align-top">
              <td className="py-1 pr-3 whitespace-nowrap text-foreground">
                {k}
                {req && <span className="text-down">*</span>}
              </td>
              <td className="py-1 pr-3 whitespace-nowrap text-muted-foreground">
                {v.enum ? (v.type ?? "string") : typeName(doc, v)}
              </td>
              <td className="py-1 text-muted-foreground">
                <Text>{v.description ?? resolve(doc, v).description}</Text>
                {v.enum && <Values lang={lang} values={v.enum} />}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Endpoint({
  lang,
  doc,
  method,
  path,
  op,
}: {
  lang: Lang;
  doc: OpenAPI;
  method: string;
  path: string;
  op: Operation;
}) {
  const t = tx(lang);
  const body = op.requestBody?.content;
  const bodyType = body ? Object.keys(body)[0] : "";
  const ok = Object.entries(op.responses).find(([code]) =>
    code.startsWith("2"),
  );
  const okResp = ok && ("$ref" in ok[1] ? undefined : ok[1]);
  const okExample = mediaExample(doc, okResp?.content);
  const errors = Object.entries(op.responses).filter(
    ([code]) => !code.startsWith("2"),
  );
  return (
    <div
      id={op.operationId}
      className="scroll-mt-20 space-y-3 border-t border-dashed pt-4"
    >
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span
          className={`w-14 font-semibold uppercase ${methodColor[method] ?? ""}`}
        >
          {method}
        </span>
        <code className="text-foreground">{path}</code>
        <span className="text-muted-foreground">{op.summary}</span>
        <span className="ml-auto border border-dashed px-1.5 text-[11px] text-muted-foreground">
          {t(...accessLabel[op["x-pingtxt-access"]])}
        </span>
      </div>
      {op.description && !op.description.startsWith("**Access:**") && (
        <p className="max-w-3xl text-muted-foreground">
          <Text>
            {op.description.replace(/(^|\n\n)\*\*Access:\*\*[\s\S]*$/, "")}
          </Text>
        </p>
      )}
      {op.parameters && op.parameters.length > 0 && (
        <div>
          <div className="mb-1 text-[11px] uppercase tracking-wider text-muted-foreground">
            {t("parameters", "parametri")}
          </div>
          <table className="w-full text-left text-[12px]">
            <tbody>
              {op.parameters.map((p) => (
                <tr
                  key={p.in + p.name}
                  className="border-b border-dashed align-top"
                >
                  <td className="py-1 pr-3 whitespace-nowrap text-foreground">
                    {p.name}
                    {p.required && <span className="text-down">*</span>}
                  </td>
                  <td className="py-1 pr-3 whitespace-nowrap text-muted-foreground">
                    {p.in} ·{" "}
                    {p.schema?.enum
                      ? (p.schema.type ?? "string")
                      : typeName(doc, p.schema)}
                  </td>
                  <td className="py-1 text-muted-foreground">
                    <Text>{p.description}</Text>
                    {p.schema?.enum && (
                      <Values lang={lang} values={p.schema.enum} />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {body && (
        <div className="space-y-2">
          <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
            {t("request body", "body della richiesta")} · {bodyType}
          </div>
          {body[bodyType].schema && (
            <Fields lang={lang} doc={doc} schema={body[bodyType].schema!} />
          )}
          {bodyType === "application/json" && (
            <CopyBlock label={t("example", "esempio")} code={mediaExample(doc, body) ?? ""} />
          )}
        </div>
      )}
      {okExample && (
        <details className="group">
          <summary className="cursor-pointer text-[11px] uppercase tracking-wider text-muted-foreground hover:text-foreground">
            {t("response", "risposta")} {ok![0]} · {okResp?.description}
          </summary>
          <div className="mt-2">
            <CopyBlock code={okExample} />
          </div>
        </details>
      )}
      {!okExample && ok && (
        <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
          {t("response", "risposta")} {ok[0]} · {okResp?.description}
        </div>
      )}
      {errors.length > 0 && (
        <div className="text-[12px] text-muted-foreground">
          {t("errors:", "errori:")}{" "}
          {errors.map(([code, r], i) => {
            const desc =
              "$ref" in r
                ? doc.components.responses[r.$ref.split("/").pop()!]
                    ?.description
                : r.description;
            return (
              <span key={code}>
                {i > 0 && " · "}
                <span className="text-foreground">{code}</span>{" "}
                {desc?.replace(/\.$/, "").toLowerCase()}
              </span>
            );
          })}
        </div>
      )}
    </div>
  );
}

export function ApiPage({ lang }: { lang: Lang }) {
  const t = tx(lang);
  const doc = loadOpenAPI();
  const byTag = new Map<string, [string, string, Operation][]>();
  for (const t of doc.tags) byTag.set(t.name, []);
  for (const [path, ops] of Object.entries(doc.paths)) {
    for (const [method, op] of Object.entries(ops)) {
      byTag.get(op.tags[0])?.push([method, path, op]);
    }
  }
  const order = ["get", "post", "put", "delete"];
  for (const list of byTag.values()) {
    list.sort(
      (a, b) =>
        a[1].localeCompare(b[1]) || order.indexOf(a[0]) - order.indexOf(b[0]),
    );
  }
  const count = [...byTag.values()].reduce((n, l) => n + l.length, 0);

  return (
    <div className="grid gap-10 py-12 lg:grid-cols-[180px_1fr]">
      <aside className="lg:sticky lg:top-20 lg:self-start">
        <div className="mb-2 text-[11px] uppercase tracking-wider text-muted-foreground">
          {t("contents", "indice")}
        </div>
        <nav className="flex flex-wrap gap-x-3 gap-y-0.5 lg:flex-col">
          <a
            href="#auth"
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            {t("authentication", "autenticazione")}
          </a>
          {doc.tags.map((t) => (
            <a
              key={t.name}
              href={`#${slug(t.name)}`}
              className="text-muted-foreground transition-colors hover:text-foreground"
            >
              {t.name.toLowerCase()}
            </a>
          ))}
        </nav>
      </aside>

      <div className="min-w-0 space-y-12">
        <div className="space-y-2">
          <Prompt>curl /api/openapi.json</Prompt>
          <h1 className="text-xl font-semibold">
            {t("api reference", "riferimento api")}
          </h1>
          <p className="max-w-2xl text-muted-foreground">
            {t(
              <>
                The {count} operations of the hub&apos;s REST API, the same the
                web UI uses. Every hub serves this document at{" "}
                <C>/api/openapi.json</C> and an interactive Swagger UI at{" "}
                <C>/api/docs</C>, where you can try requests with your session
                or a token. Download it here as{" "}
                <a href="/openapi.json" className={link}>
                  openapi.json
                </a>{" "}
                (OpenAPI 3.1, latest version on{" "}
                <a
                  href={`${REPO}/blob/main/internal/hub/openapi.json`}
                  className={link}
                >
                  main
                </a>
                ) to generate a client.
              </>,
              <>
                Le {count} operazioni della REST API dell&apos;hub, le stesse
                che usa l&apos;interfaccia web. Ogni hub serve questo documento
                su <C>/api/openapi.json</C> e una Swagger UI interattiva su{" "}
                <C>/api/docs</C>, dove puoi provare le richieste con la tua
                sessione o un token. Scaricalo qui come{" "}
                <a href="/openapi.json" className={link}>
                  openapi.json
                </a>{" "}
                (OpenAPI 3.1, ultima versione su{" "}
                <a
                  href={`${REPO}/blob/main/internal/hub/openapi.json`}
                  className={link}
                >
                  main
                </a>
                ) per generare un client. Le descrizioni degli endpoint vengono
                dal documento OpenAPI e sono in inglese.
              </>,
            )}
          </p>
        </div>

        <section
          id="auth"
          className="space-y-3 [&_li]:ml-4 [&_li]:list-['-_'] [&_p]:text-muted-foreground [&_ul]:space-y-1 [&_ul]:text-muted-foreground"
        >
          <SectionTitle id="auth">
            {t("authentication", "autenticazione")}
          </SectionTitle>
          <p>
            {t(
              <>
                Create a token in{" "}
                <b className="text-foreground">settings → api tokens</b> and
                send it as a bearer token. Bodies are JSON, times are Unix
                milliseconds, errors are <C>{`{"error": "..."}`}</C>.
              </>,
              <>
                Crea un token in{" "}
                <b className="text-foreground">impostazioni → token api</b> e
                mandalo come bearer token. I body sono JSON, gli orari sono
                millisecondi Unix, gli errori sono <C>{`{"error": "..."}`}</C>.
              </>,
            )}
          </p>
          <CopyBlock
            code={`curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/monitors

curl -X POST -H "Authorization: Bearer ptk_..." -H "Content-Type: application/json" \\
  -d '{"name":"Website","type":"http","target":"https://example.com"}' \\
  https://status.example.com/api/monitors`}
          />
          <p>
            {t(
              "Each endpoint is marked with who can call it:",
              "Ogni endpoint indica chi può chiamarlo:",
            )}
          </p>
          <ul>
            {accessHelp.map(([a, en, it]) => (
              <li key={a}>
                <span className="text-foreground">
                  {t(...accessLabel[a])}
                </span>
                : {t(en, it)}
              </li>
            ))}
          </ul>
          <p>
            {t(
              <>
                Viewers (read-only users) see passwords in monitor targets,
                headers and notification channels as <C>********</C>.
              </>,
              <>
                Gli utenti in sola lettura vedono le password nelle destinazioni
                dei monitor, negli header e nei canali di notifica come{" "}
                <C>********</C>.
              </>,
            )}
          </p>
        </section>

        {doc.tags.map((t) => (
          <section key={t.name} className="space-y-4">
            <SectionTitle id={slug(t.name)}>
              {t.name.toLowerCase()}
            </SectionTitle>
            {t.description && (
              <p className="text-muted-foreground">{t.description}</p>
            )}
            {(byTag.get(t.name) ?? []).map(([method, path, op]) => (
              <Endpoint
                key={method + path}
                lang={lang}
                doc={doc}
                method={method}
                path={path}
                op={op}
              />
            ))}
          </section>
        ))}
      </div>
    </div>
  );
}
