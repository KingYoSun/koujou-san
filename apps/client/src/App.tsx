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
      <p className="note">
        Contextの取得・保存・入力選択と、対象作業の実行は別の操作です。
        APIの表示はプロセスの起動確認です。
      </p>
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
      <footer>
        <details>
          <summary>このクライアントでの実行について</summary>
          <p>Contextの編集・所属変更・プレビューから実行は始まりません。</p>
          <p>
            {native
              ? "画面を閉じても、独立Runnerの許可済みの実行は継続します。"
              : "Web版はローカルのCLI実行機構を持ちません。"}
          </p>
        </details>
      </footer>
    </main>
  );
}
