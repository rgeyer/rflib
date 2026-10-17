import { useQuery } from "@tanstack/react-query";
import { api, call, type User } from "../api/client";

export type AuthInfo = { accounts: boolean; signup?: "open" | "invite" | "closed"; user?: User };

/** useAuth says whether the server has accounts, who may register, and
 * who is signed in. */
export function useAuth() {
  return useQuery({
    queryKey: ["auth"],
    queryFn: () => call<AuthInfo>(api.GET("/auth")),
    staleTime: Infinity,
  });
}

/** useMaintainer says whether the user may change the shared list: an
 * admin, or anyone on a server without accounts. */
export function useMaintainer(): boolean {
  const auth = useAuth().data;
  return !auth?.accounts || auth.user?.role === "admin";
}
