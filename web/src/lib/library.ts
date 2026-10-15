import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { api, call, type Channel, type Library } from "../api/client";

/** useLib is the library in the URL (/l/:lib/...). */
export function useLib(): string {
  return useParams().lib ?? "";
}

/** libPath encodes a library name for API paths and links. */
export function enc(s: string): string {
  return encodeURIComponent(s);
}

/** useTags lists the tags worth offering in a library: the curated shared tags and the library's own. */
export function useTags(lib: string): string[] {
  const curated = useQuery({
    queryKey: ["tags"],
    queryFn: () => call<{ tags: { name: string }[] }>(api.GET("/tags")),
    staleTime: Infinity,
  });
  const channels = useQuery({
    queryKey: ["channels", lib, ""],
    queryFn: () => call<{ channels: Channel[]; total: number }>(api.GET("/libraries/{lib}/channels", { params: { path: { lib }, query: { q: "" } } })),
  });
  const all = new Set<string>(curated.data?.tags.map((t) => t.name));
  for (const c of channels.data?.channels ?? []) for (const t of [...c.tags, ...(c.link?.tags ?? [])]) all.add(t);
  return [...all].sort((a, b) => a.localeCompare(b));
}

/** useLibraries names the user's own library and the shared list, or null
 * until they've loaded. */
export function useLibraries(): { mine: string | null; shared: string | null } {
  const libraries = useQuery({
    queryKey: ["libraries"],
    queryFn: () => call<{ libraries: Library[] }>(api.GET("/libraries")),
  });
  const all = libraries.data?.libraries ?? [];
  return { mine: all.find((l) => l.default)?.name ?? null, shared: all.find((l) => l.shared)?.name ?? null };
}
