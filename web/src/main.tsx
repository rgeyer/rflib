import "@mantine/core/styles.css";
import "@mantine/notifications/styles.css";
import { MantineProvider } from "@mantine/core";
import { ModalsProvider } from "@mantine/modals";
import { Notifications } from "@mantine/notifications";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { STALE_DRAFT, UNAUTHORIZED } from "./api/client";
import { App } from "./App";

const queries = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } });
// A change made against an out-of-date draft was refused: show what's
// there now.
window.addEventListener(STALE_DRAFT, () => void queries.invalidateQueries());
// A session that ended shows sign-in again, on a server with accounts.
window.addEventListener(UNAUTHORIZED, () => void queries.invalidateQueries({ queryKey: ["auth"] }));

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <MantineProvider defaultColorScheme="auto">
      <QueryClientProvider client={queries}>
        <ModalsProvider>
          <Notifications />
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </ModalsProvider>
      </QueryClientProvider>
    </MantineProvider>
  </StrictMode>,
);
