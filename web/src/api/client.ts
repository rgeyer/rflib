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
export type Section = S["Section"];
export type Search = S["Search"];
export type Model = S["Model"];
export type Seed = S["Seed"];
export type Change = S["Change"];
export type Conversion = S["Conversion"];
export type AddResult = S["AddResult"];
export type MemoryDiff = S["MemoryDiff"];
export type MemoryValues = S["MemoryValues"];
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
export type Draft = NonNullable<S["Draft"]>;
export type ListChanges = S["ListChanges"];
export type User = S["User"];

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

// Each radio page says which draft it shows (or "none"), and changes to
// that radio send it: the server opens a draft with the first change, and
// refuses a change made against a draft that's no longer open (see
// STALE_DRAFT). Other pages send "any". A change's response names the
// draft it went to.
const shownDrafts = new Map<string, string>();
const DRAFT_HEADER = "X-Rflib-Draft";

function radioOf(url: string): string | null {
  const m = /\/libraries\/([^/]+)\/radios\/([^/]+)\//.exec(new URL(url, location.href).pathname);
  return m ? `${decodeURIComponent(m[1])}/${decodeURIComponent(m[2])}` : null;
}

/** showDraft records the draft a radio's page shows; null for none. */
export function showDraft(lib: string, radio: string, id: string | null) {
  shownDrafts.set(`${lib}/${radio}`, id ?? "none");
}

const drafts: Middleware = {
  onRequest({ request }) {
    // From a page that doesn't show the radio (such as Add to radio on the
    // Channels page), a change goes to its draft, opening one if needed.
    const key = request.method === "GET" ? null : radioOf(request.url);
    if (key) request.headers.set(DRAFT_HEADER, shownDrafts.get(key) ?? "any");
    return request;
  },
  onResponse({ request, response }) {
    const key = radioOf(request.url);
    const id = response.headers.get(DRAFT_HEADER);
    if (key && id && response.ok) shownDrafts.set(key, id);
    return response;
  },
};

export const api = createClient<paths>({ baseUrl: "/api/v1" });
api.use(auth);
api.use(drafts);

/** STALE_DRAFT is dispatched on window when a change was made against a
 * radio's draft that's no longer the one open; the page reloads. */
export const STALE_DRAFT = "rflib:stale-draft";

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
    if (body?.error?.code === "stale_draft") window.dispatchEvent(new Event(STALE_DRAFT));
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
