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

  const account = <Account native={native} />;
  const connection = (
    <section className="panel" aria-labelledby="environment-title">
      <h2 id="environment-title">環境の状態</h2>
      <dl className="status-list">
        <div>
          <dt>クライアント</dt>
          <dd>{native ? "Tauri ネイティブ" : "Web"}</dd>
        </div>
        <div>
          <dt>Go API</dt>
          <dd role="status" aria-live="polite">
            {health.isPending
              ? "確認中"
              : health.isError
                ? "接続できません"
                : "起動を確認しました"}
          </dd>
        </div>
        <div>
          <dt>共有状態</dt>
          <dd>Go API・PostgreSQLで確定</dd>
        </div>
      </dl>
    </section>
  );
  return (
    <main className="shell">
      <header className="brand">
        <h1>koujou-san</h1>
        <p>Software Factory Orchestra</p>
      </header>
      {typeof location !== "undefined" &&
      location.pathname.startsWith("/auth/") ? (
        account
      ) : (
        <Team
          native={native}
          account={account}
          connection={connection}
          health={
            health.isPending
              ? "確認中"
              : health.isError
                ? "接続できません"
                : "接続確認済み"
          }
        />
      )}
      <footer></footer>
    </main>
  );
}
