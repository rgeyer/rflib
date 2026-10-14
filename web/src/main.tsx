import "@mantine/core/styles.css";
import "@mantine/notifications/styles.css";
import { MantineProvider } from "@mantine/core";
import { ModalsProvider } from "@mantine/modals";
import { Notifications } from "@mantine/notifications";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "./App";

const queries = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } });

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
