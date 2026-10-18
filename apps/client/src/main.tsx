import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { DesktopConnection } from "./desktop-connection";
import { clientCapabilities } from "./capabilities";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing root element");

const queryClient = new QueryClient();
const apiBaseUrl = import.meta.env.VITE_API_BASE_URL || "";
const native =
  clientCapabilities("__TAURI_INTERNALS__" in window).surface === "native";

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      {native ? (
        <DesktopConnection>
          <App apiBaseUrl={apiBaseUrl} native={native} />
        </DesktopConnection>
      ) : (
        <App apiBaseUrl={apiBaseUrl} native={native} />
      )}
    </QueryClientProvider>
  </StrictMode>,
);
