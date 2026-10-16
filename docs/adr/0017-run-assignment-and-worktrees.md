# 0017: Runの割当と専用worktree

- 状態: Accepted
- 日付: 2026-10-17
- 対象: #17、設計書§6.4・11.4・12.3・12.5、[計画](../progress/2026-10-17-issue-17-plan.md)

## 割当の確定

Runの編集と実行依頼は別権限とする。依頼者の現在の実行許可、Runと固定入力の閲覧、登録端末の有効性・入力取得/報告許可、登録者の所属、端末が公開した利用方針を同じWorkspace transactionで照合する。端末はUserを名乗らず、端末資格情報で自分のRunnerと接続能力を報告する。

接続能力はkind/target、版、環境、CLI認証主体への論理参照、確認状況、入力方式、能力、許可する利用者とrepositoryを持つ。名前だけで能力を保証しない。未確認・能力不足・端末不在は起動前の準備待ちとして記録する。現在許可されていない依頼は割当を作らず拒否する。

割当はRunの固定input参照と、依頼者・端末・Runner・接続版・世代を持つ。`Store.Apply`の現在認可、CAS、receiptを使い、二端末からの競合は一つへ収束する。競合したクライアントは返された現在版に従ってRunを再取得する。Runの履歴へ割当を記録し、入力本文を状態更新のたびに複製しない。

## 期限と報告

Runnerの接続確認と割当の有効期間は2分とする。更新時は現在の端末・依頼者・入力・利用方針を再照合する。同じ世代のrenewは接続確認を更新するが、CLIの稼働を観測したことにはしない。期限切れだけで停止・失敗・再配布へ進めない。

assigned-inputとRun報告では、端末資格情報に加えて現在の割当世代を確認する。報告はreceiptの再生前とtransaction内でも確認し、古い世代や取消済み範囲を拒否する。`prepared`は準備完了、`started`はプロセス開始の別報告である。期限切れや許可取消後も、開始済みCLIが停止したとは表示しない。

既存の`assigned_device`は共通認可の内部列として使う。旧Run fixtureの明示割当は維持するが、製品へ旧方式の割当APIを追加しない。未確定sync-reportはRunの起動確認へ昇格しない。

## Runごとの作業領域

ユーザー判断により、repositoryを使うRunごとにworktreeとbranchを自動作成する。共有側はrepositoryの論理ID、固定commit、Run固有の領域IDとbranchを保持する。端末側の設定だけがrepositoryの実pathを持つ。

ネイティブは有効な割当を取得し、設定したrepositoryの固定commitから専用worktreeを作る。再試行では同じ領域とbranchを確認し、編集済みファイルを初期化しない。元checkoutや既存branchを動かさず、終了時も成果を自動削除しない。Gitを使わないcommandへrepositoryを要求しない。

Git操作後に現在の割当・入力を再確認してからpreparedを報告する。Windowsのcanonical pathが返すverbatim prefixは、Gitへ渡すときに通常の形式へ戻す。作成先は設定されたapp-data pathを保ち、packaged appのjunctionを解決して長い実pathへ変えない。失敗でbranchだけが残った再試行では、固定commitが一致するbranchを再利用し、既存branchをresetしない。実CLI起動・対話・プロセス復旧は#18以降で接続し、preparedから仮の起動報告を生成しない。
