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
import { pageMeta } from "@/lib/seo";

export const metadata: Metadata = pageMeta({
  title: "REST API reference",
  description:
    "Every endpoint of the pingtxt REST API: authentication with API tokens, roles, parameters and examples.",
  path: "/docs/api/",
});

const methodColor: Record<string, string> = {
  get: "text-up",
  post: "text-blue-500",
  put: "text-amber-500",
  delete: "text-down",
};

const accessLabel: Record<Access, string> = {
  public: "public",
  read: "read token",
  write: "write token · admin",
  session: "browser session",
  admin: "admin session",
};

const accessHelp: [Access, string][] = [
  ["public", "no authentication"],
  ["read", "any user or API token, a read token is enough"],
  [
    "write",
    "an admin, with a write token or the browser session; viewers and read tokens get 403",
  ],
  ["session", "browser session only, API tokens get 403"],
  ["admin", "browser session of an admin only"],
];

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

function Values({ values }: { values: unknown[] }) {
  return (
    <span className="block">
      one of:{" "}
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

function Fields({ doc, schema }: { doc: OpenAPI; schema: Schema }) {
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
                {v.enum && <Values values={v.enum} />}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Endpoint({
  doc,
  method,
  path,
  op,
}: {
  doc: OpenAPI;
  method: string;
  path: string;
  op: Operation;
}) {
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
          {accessLabel[op["x-pingtxt-access"]]}
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
            parameters
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
                    {p.schema?.enum && <Values values={p.schema.enum} />}
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
            request body · {bodyType}
          </div>
          {body[bodyType].schema && (
            <Fields doc={doc} schema={body[bodyType].schema!} />
          )}
          {bodyType === "application/json" && (
            <CopyBlock label="example" code={mediaExample(doc, body) ?? ""} />
          )}
        </div>
      )}
      {okExample && (
        <details className="group">
          <summary className="cursor-pointer text-[11px] uppercase tracking-wider text-muted-foreground hover:text-foreground">
            response {ok![0]} · {okResp?.description}
          </summary>
          <div className="mt-2">
            <CopyBlock code={okExample} />
          </div>
        </details>
      )}
      {!okExample && ok && (
        <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
          response {ok[0]} · {okResp?.description}
        </div>
      )}
      {errors.length > 0 && (
        <div className="text-[12px] text-muted-foreground">
          errors:{" "}
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

export default function ApiReference() {
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
          contents
        </div>
        <nav className="flex flex-wrap gap-x-3 gap-y-0.5 lg:flex-col">
          <a
            href="#auth"
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            authentication
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
          <h1 className="text-xl font-semibold">api reference</h1>
          <p className="max-w-2xl text-muted-foreground">
            The {count} operations of the hub&apos;s REST API, the same the web
            UI uses. Every hub serves this document at{" "}
            <code className="bg-muted px-1 text-foreground">
              /api/openapi.json
            </code>{" "}
            and an interactive Swagger UI at{" "}
            <code className="bg-muted px-1 text-foreground">/api/docs</code>,
            where you can try requests with your session or a token. Download it
            here as{" "}
            <a
              href="/openapi.json"
              className="text-foreground underline underline-offset-4"
            >
              openapi.json
            </a>{" "}
            (OpenAPI 3.1, latest version on{" "}
            <a
              href={`${REPO}/blob/main/internal/hub/openapi.json`}
              className="text-foreground underline underline-offset-4"
            >
              main
            </a>
            ) to generate a client.
          </p>
        </div>

        <section
          id="auth"
          className="space-y-3 [&_li]:ml-4 [&_li]:list-['-_'] [&_p]:text-muted-foreground [&_ul]:space-y-1 [&_ul]:text-muted-foreground"
        >
          <SectionTitle id="auth">authentication</SectionTitle>
          <p>
            Create a token in{" "}
            <b className="text-foreground">settings → api tokens</b> and send it
            as a bearer token. Bodies are JSON, times are Unix milliseconds,
            errors are{" "}
            <code className="bg-muted px-1 text-foreground">{`{"error": "..."}`}</code>
            .
          </p>
          <CopyBlock
            code={`curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/monitors

curl -X POST -H "Authorization: Bearer ptk_..." -H "Content-Type: application/json" \\
  -d '{"name":"Website","type":"http","target":"https://example.com"}' \\
  https://status.example.com/api/monitors`}
          />
          <p>Each endpoint is marked with who can call it:</p>
          <ul>
            {accessHelp.map(([a, text]) => (
              <li key={a}>
                <span className="text-foreground">{accessLabel[a]}</span>:{" "}
                {text}
              </li>
            ))}
          </ul>
          <p>
            Viewers (read-only users) see passwords in monitor targets, headers
            and notification channels as{" "}
            <code className="bg-muted px-1 text-foreground">********</code>.
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
