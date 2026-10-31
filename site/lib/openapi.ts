import { readFileSync } from "node:fs";
import path from "node:path";

// The OpenAPI document lives next to the Go routes (internal/hub/openapi.json)
// and is read at build time, so the site always matches the hub on main.
export const OPENAPI_PATH = path.join(
  process.cwd(),
  "..",
  "internal",
  "hub",
  "openapi.json",
);

export type Schema = {
  $ref?: string;
  type?: string;
  format?: string;
  description?: string;
  enum?: unknown[];
  const?: unknown;
  default?: unknown;
  examples?: unknown[];
  items?: Schema;
  properties?: Record<string, Schema>;
  required?: string[];
  additionalProperties?: Schema | boolean;
  allOf?: Schema[];
  oneOf?: Schema[];
};

type Media = { schema?: Schema; example?: unknown };
type Content = Record<string, Media>;

export type Parameter = {
  name: string;
  in: string;
  required?: boolean;
  description?: string;
  schema?: Schema;
};

export type Operation = {
  tags: string[];
  summary: string;
  description?: string;
  operationId: string;
  "x-pingtxt-access": Access;
  parameters?: Parameter[];
  requestBody?: { content: Content };
  responses: Record<
    string,
    { description: string; content?: Content } | { $ref: string }
  >;
};

export type Access = "public" | "read" | "write" | "session" | "admin";

export type OpenAPI = {
  info: { title: string; description: string };
  tags: { name: string; description?: string }[];
  paths: Record<string, Record<string, Operation>>;
  components: {
    schemas: Record<string, Schema>;
    responses: Record<string, { description: string; content?: Content }>;
  };
};

export function loadOpenAPI(): OpenAPI {
  return JSON.parse(readFileSync(OPENAPI_PATH, "utf8"));
}

export function rawOpenAPI(): string {
  return readFileSync(OPENAPI_PATH, "utf8");
}

export function resolve(doc: OpenAPI, s: Schema): Schema {
  let cur = s;
  for (let i = 0; cur.$ref && i < 10; i++) {
    cur = doc.components.schemas[cur.$ref.split("/").pop()!] ?? {};
  }
  return cur;
}

// example builds a sample value from a schema.
export function example(
  doc: OpenAPI,
  s: Schema | undefined,
  depth = 0,
): unknown {
  if (!s || depth > 6) return null;
  if (s.$ref) return example(doc, resolve(doc, s), depth + 1);
  if (s.examples?.length) return s.examples[0];
  if (s.const !== undefined) return s.const;
  if (s.enum?.length)
    return s.enum[0] === "" && s.enum.length > 1 ? s.enum[1] : s.enum[0];
  if (s.allOf)
    return Object.assign(
      {},
      ...s.allOf.map((x) => example(doc, x, depth + 1) as object),
    );
  if (s.oneOf) return example(doc, s.oneOf[0], depth + 1);
  switch (s.type) {
    case "object": {
      const out: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(s.properties ?? {}))
        out[k] = example(doc, v, depth + 1);
      if (!s.properties && typeof s.additionalProperties === "object")
        out.key = example(doc, s.additionalProperties, depth + 1);
      return out;
    }
    case "array":
      return [example(doc, s.items, depth + 1)];
    case "integer":
      return s.format === "int64" && /unix time/i.test(s.description ?? "")
        ? 1761868800000
        : 1;
    case "number":
      return 12.5;
    case "boolean":
      return true;
    case "string":
      return s.format === "binary" ? "<binary>" : "string";
  }
  return null;
}

// mediaExample returns the example of a JSON body, or one built from its schema.
export function mediaExample(
  doc: OpenAPI,
  c: Content | undefined,
): string | null {
  if (!c) return null;
  const [type, m] = Object.entries(c)[0];
  if (type !== "application/json")
    return typeof m.example === "string" ? m.example : null;
  const v = m.example ?? example(doc, m.schema);
  return JSON.stringify(v, null, 2);
}

export function typeName(doc: OpenAPI, s: Schema | undefined): string {
  if (!s) return "";
  if (s.$ref) return s.$ref.split("/").pop()!;
  if (s.oneOf) return s.oneOf.map((x) => typeName(doc, x)).join(" | ");
  if (s.type === "array") return typeName(doc, s.items) + "[]";
  if (s.enum) return s.enum.map((e) => JSON.stringify(e)).join(" | ");
  return s.type ?? "object";
}
