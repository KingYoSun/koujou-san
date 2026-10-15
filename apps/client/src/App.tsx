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

  return (
    <main className="shell">
      <header>
        <p className="eyebrow">Software Factory Orchestra</p>
        <h1>koujou-san</h1>
        <p className="introduction">開発環境の起動確認</p>
      </header>
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
            <dt>実装段階</dt>
            <dd>環境構築</dd>
          </div>
        </dl>
        <p className="note">
          段階Aの認証・共有状態・検索・観測機能は、これから実装します。
          APIの表示はプロセスの起動確認です。
        </p>
      </section>
      <Account native={native} />
      <Team native={native} />
      <footer>
        <p>Issueの受入条件と設計書を元に、ここから実装を始めます。</p>
        <p>
          {native
            ? "ネイティブの実行機構は未実装です。"
            : "Web版はローカルのCLI実行機構を持ちません。"}
        </p>
      </footer>
    </main>
  );
}
