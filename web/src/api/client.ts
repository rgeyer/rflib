// The API client: typed from the OpenAPI document (npm run gen writes
// schema.d.ts from ../internal/api/openapi.yaml).
import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

type S = components["schemas"];
export type Channel = S["Channel"];
export type Library = S["Library"];
export type Radio = S["Radio"];
export type Memory = S["Memory"];
export type Source = S["Source"];
export type Search = S["Search"];
export type Model = S["Model"];
export type Seed = S["Seed"];
export type Change = S["Change"];
export type Conversion = S["Conversion"];
export type AddResult = S["AddResult"];
export type ImportResult = S["ImportResult"];
export type ChannelInput = S["ChannelInput"];
export type Selection = S["Selection"];
export type Organization = S["Organization"];
export type OrgRef = S["OrgRef"];
export type System = S["System"];
export type SystemMember = S["SystemMember"];
export type Plan = S["Plan"];
export type PlanEntry = S["PlanEntry"];
export type PlanRow = S["PlanRow"];
export type RecordRevision = S["RecordRevision"];

const TOKEN_KEY = "rflib.token";

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setToken(token: string) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    // Without storage the token lasts for this page only.
  }
  sessionToken = token;
}

let sessionToken = "";

const auth: Middleware = {
  onRequest({ request }) {
    const token = getToken() || sessionToken;
    if (token) request.headers.set("Authorization", `Bearer ${token}`);
    return request;
  },
};

export const api = createClient<paths>({ baseUrl: "/api/v1" });
api.use(auth);

/** ApiError is an error response: its status, code, and message. */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

/** UNAUTHORIZED is dispatched on window when the API wants a token. */
export const UNAUTHORIZED = "rflib:unauthorized";

type Result<T> = { data?: T; error?: unknown; response: Response };

/** call returns a request's data, or throws its error as an ApiError. */
export async function call<T>(request: Promise<Result<T>>): Promise<T> {
  const { data, error, response } = await request;
  if (!response.ok) {
    if (response.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED));
    const body = error as { error?: { code?: string; message?: string } } | undefined;
    throw new ApiError(response.status, body?.error?.code ?? "internal", body?.error?.message ?? response.statusText);
  }
  return data as T;
}

/** download fetches a file from the API and saves it, returning the
 * response's headers (which say what was left out). */
export async function download(path: string): Promise<Headers> {
  const headers: Record<string, string> = {};
  const token = getToken() || sessionToken;
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`/api/v1${path}`, { headers });
  if (!res.ok) {
    if (res.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED));
    const body = (await res.json().catch(() => ({}))) as { error?: { code?: string; message?: string } };
    throw new ApiError(res.status, body.error?.code ?? "internal", body.error?.message ?? res.statusText);
  }
  const blob = await res.blob();
  const name = /filename="?([^";]+)"?/.exec(res.headers.get("Content-Disposition") ?? "")?.[1] ?? "export.csv";
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
  return res.headers;
}

/** message is an error's text for a notification. */
export function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
