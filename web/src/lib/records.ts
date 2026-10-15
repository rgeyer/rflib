// Organizations, systems, and plans live in the shared list; these are
// the helpers their pages share.
import { useQuery } from "@tanstack/react-query";
import { api, call, type Organization, type Plan, type System } from "../api/client";

/** networkURL is a page about a node on its network's own site; rflib
 * fetches nothing from it. AllStar has a page per node; EchoLink and IRLP
 * have lookups. */
export function networkURL(id: string): string | null {
  const [net, node] = id.split(":");
  switch (net) {
    case "allstar":
      return `https://stats.allstarlink.org/stats/${encodeURIComponent(node)}`;
    case "echolink":
      return "https://www.echolink.org/validation/node_lookup.jsp";
    case "irlp":
      return "http://status.irlp.net/";
  }
  return null;
}

/** NETWORK_LABELS name the networks as their operators write them. */
export const NETWORK_LABELS: Record<string, string> = { allstar: "AllStar", echolink: "EchoLink", irlp: "IRLP", "wires-x": "WIRES-X" };

export function networkLabel(id: string): string {
  const [net, node] = id.split(":");
  return `${NETWORK_LABELS[net] ?? net} ${node}`;
}

export const ORG_KINDS = ["club", "emergency", "agency", "individual"];
export const LINK_TYPES = ["", "allstar", "echolink", "irlp", "wires-x", "rf", "other"];
export const PLAN_STATUSES = ["draft", "published", "archived"];

export function useOrgs() {
  return useQuery({ queryKey: ["organizations"], queryFn: () => call<{ organizations: Organization[] }>(api.GET("/organizations")) });
}

export function useSystems() {
  return useQuery({ queryKey: ["systems"], queryFn: () => call<{ systems: System[] }>(api.GET("/systems")) });
}

export function usePlans() {
  return useQuery({ queryKey: ["plans"], queryFn: () => call<{ plans: Plan[] }>(api.GET("/plans")) });
}

/** planDates is a plan's dates for reading: "2026-07-15 to 2026-07-26". */
export function planDates(p: Plan): string {
  if (p.starts && p.ends) return p.starts === p.ends ? p.starts : `${p.starts} to ${p.ends}`;
  return p.starts ? `from ${p.starts}` : p.ends ? `until ${p.ends}` : "";
}

export type Point = [number, number];

/** systemLines are the lines between a system's located members: every
 * pair, dashed when either is linked only on demand. where finds a
 * member's position by its shared channel ID. */
export function systemLines(x: System, where: (sharedID: string) => Point | null): { from: Point; to: Point; onDemand: boolean }[] {
  const located = x.members.flatMap((m) => {
    const p = where(m.channel_id);
    return p ? [{ p, onDemand: m.linking === "on-demand" }] : [];
  });
  const out = [];
  for (let i = 0; i < located.length; i++)
    for (let j = i + 1; j < located.length; j++) out.push({ from: located[i].p, to: located[j].p, onDemand: located[i].onDemand || located[j].onDemand });
  return out;
}
