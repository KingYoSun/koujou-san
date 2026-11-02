import { useQuery } from "@tanstack/react-query";
import { fetchHealth } from "./health";
import { Account } from "./auth";
import { Team } from "./team";

type AppProps = {
  apiBaseUrl: string;
  native: boolean;
};

export function App({ apiBaseUrl, native }: AppProps) {
  const health = useQuery({
    queryKey: ["api-health", apiBaseUrl],
    queryFn: ({ signal }) => fetchHealth(apiBaseUrl, signal, native),
    retry: false,
    refetchInterval: 10_000,
  });
  const healthText = health.isPending
    ? "確認中"
    : health.isError
      ? "接続できません"
      : "接続確認済み";

  const account = <Account native={native} />;
  const connection = (
    <section aria-label="API接続">
      <dl className="status-list">
        <div>
          <dt>クライアント</dt>
          <dd>{native ? "Windowsアプリ" : "Web"}</dd>
        </div>
        <div>
          <dt>Go API</dt>
          <dd role="status" aria-live="polite">
            {healthText}
          </dd>
        </div>
      </dl>
    </section>
  );
  return (
    <main className="shell">
      <header className="brand">
        <h1>koujou-san</h1>
      </header>
      {typeof location !== "undefined" &&
      location.pathname.startsWith("/auth/") ? (
        account
      ) : (
        <Team
          native={native}
          account={account}
          connection={connection}
          health={healthText}
        />
      )}
    </main>
  );
}
