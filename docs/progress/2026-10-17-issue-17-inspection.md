# Issue #17の独立検品

## 対象と入力

- 初回対象commit: `60180c067de945aef41a692bed9296311029ab64`。
- 差分検品対象commit: `5054f09e68e4073517ab1d9d78d2c229e05f4296`。現在の判定はこのcommitを対象とする。
- 比較基準main: `c4d1e27322ab4c66bd9679f5fc33ff1f07c8ad54`。
- 関連Issue: [#17](https://github.com/KingYoSun/koujou-san/issues/17)、Scope revision 1。
- 検品者: 実装と分離した独立担当。実施日: 2026-10-17。
- 入力: Issue本文、AGENTS.md、AGENTS.local.md、docs/README.md、開発手順、Issue手順、[計画](2026-10-17-issue-17-plan.md)、[ADR 0017](../adr/0017-run-assignment-and-worktrees.md)、設計書§6.4・11.4・12.3・12.5、固定commitの差分とコード、[実装側証跡](2026-10-17-issue-17-evidence.md)。実装側の試験結果は独立試験の代わりにしていない。
- 対象: 割当・権限・端末能力・世代・固定入力、準備と起動報告の区別、ネイティブでの実Git worktree作成。
- 対象外: 実CLI起動・対話・プロセス復旧、期限だけによる自動再配布。repositoryを使うRunごとのworktree・branch自動作成を採用し、直列化案は判定対象にしない。
- 実行環境: Windows、PowerShell、専用PostgreSQL fixture、Git for Windows、リポジトリ指定のGo・Rust・pnpm。コード調査は既存CodeGraphから開始した。
- 情報の分離: 実装者の会話と自己評価は入力として継承していない。端末資格情報、OAuth情報、UI fixtureの私用metadataは出力していない。

検品開始時と独立試験後にHEADが対象commitであること、tracked fileに差分がないことを確認した。製品ソースとGitの状態は変更していない。INVAR-3のみ、独立試験を一時Go overlayへ追加した。

## 条件ごとの結果

下表のAPI試験は独立実行した`test:runs`、認可試験は`test:access`、Git試験は`test:desktop`を指す。全末端の入力と判定を列挙し、画面は実操作画像・HTTP記録と照合した。

| 条件・末端 | 確認した経路と結果 | 証拠 | 判定 |
| --- | --- | --- | --- |
| AC-1 許可された依頼と主体 | 実行許可を持つ依頼者から許可端末へ割り当て、依頼者・端末・Runner・接続主体・固定input・世代を保存。開始時刻は未生成 | assignment.go:186–234、assignment_test.go:93–128、独立API試験 | PASS |
| AC-1 共有画面 | Windowsの準備成功画面とWebの同一Run表示で、依頼者・端末・Runner・CLI主体・世代・期限・準備時刻・branch・固定commitをHTTP記録と照合。Webからの別Runへの割当も表示と保存版が一致 | native-prepared/second画像とJSON、web-shared/assigned画像とJSON、下記の実操作照合 | PASS |
| AC-2 同時要求 | 同一Run・同一基準版から二端末へ同時依頼し、成功一件と競合一件へ収束。WorkspaceのtransactionとCASで二重確定を防ぐ | assignment_test.go:109–128、Store.Apply:179–219、独立API試験 | PASS |
| AC-2 再送と現在版 | 成功要求の同一再送は同一応答。双方の現在版取得が同じRun参照となる | assignment_test.go:130–144、独立API試験 | PASS |
| AC-3 ログイン・参加・登録と実行許可 | 非メンバー、実行許可のない参加者を拒否。既存の認可試験でも登録・独立実行権限・端末主体を分けて確認 | assignment_test.go:146–152、access/service.go:154–243、独立API・認可試験 | PASS |
| AC-3 端末方針不一致 | 許可利用者の不一致を未確認能力で迂回できず403。拒否後は割当なし | assignment.go:150–184、assignment_test.go:220–231、独立API試験 | PASS |
| AC-4 能力未確認 | `capability_unconfirmed`、割当なし、開始時刻なし | assignment_test.go:233–253、独立API試験 | PASS |
| AC-4 必要能力不足 | `capability_missing`と`binding_missing`、割当なし、開始時刻なし | 同上 | PASS |
| AC-4 端末不在 | 接続期限切れを`device_absent`として保存し、割当と開始時刻を生成しない | 同上 | PASS |
| AC-5a 古い割当世代 | 古い世代による入力取得・報告を拒否。報告はreceipt再生前とtransaction内の双方で現在条件を確認 | assignment_test.go:160–166・191–201、service.go:645–650、Store.Apply:189–205、独立API試験 | PASS |
| AC-5a 資格情報とWorkspace許可取消 | 端末資格情報の更新・失効、登録者の所属、依頼者の実行許可、端末許可、固定入力依存の閲覧取消を照合し、新しい要求を拒否 | assignment.go:241–280、assignment_test.go:277–291、独立API・認可試験 | PASS |
| AC-5a 現在の端末方針 | 許可利用者を取り消した後の新しい報告を拒否 | assignment_test.go:212–218、独立API試験 | PASS |
| AC-5b 期限切れと停止 | 開始報告後の期限切れでもRunning・Unknown・担当端末を保持し、終了時刻を作らない。同じ世代のrenewもKnownを捏造しない | assignment_test.go:186–210、独立API試験 | PASS |
| AC-5b 取消と停止・再配布 | 許可取消だけでFinishedを作らない。既に割当を持つRunは新たにAssignできず、期限だけの再配布経路もない | assignment.go:195–196、assignment_test.go:282–295、独立API試験 | PASS |
| AC-6 二Runの共有記録 | 同じrepositoryに対してRunごとに異なる領域ID・branchを保存 | assignment.go:222–227、assignment_test.go:154–158、独立API試験 | PASS |
| AC-6 実Gitの分離と再送 | 所有する一時repositoryから二つのworktreeとbranchを作成。一方の編集は他方・元checkoutへ影響しない。同じ領域への再試行は編集を保持し、元HEAD・branchも維持。差分検品では固定commitにあるbranchだけが残る状態からの準備も成功 | 5054f09のrunner.rs:68–124とGit試験、独立`test:desktop`再実行 | PASS |
| AC-6 APIからネイティブ準備まで | 同じrepositoryを使う二Runを実Windows画面から準備。APIの領域・branch・固定commitを画像・保存記録・残存する実Gitと独立に照合し一致。再準備後の編集hash保持と元checkout不変も確認 | 下記の実操作照合、git-worktrees.json、実Gitの独立読取り | PASS |
| AC-7 固定input・成果物 | 割当のinput参照と固定Specを比較し、未取得成果物の状態も保持 | assignment_test.go:168–172、独立API試験 | PASS |
| AC-7 固定Manifest | 固定Manifest参照と本文を担当端末へ渡し、固定入力の依存権限取消後は取得を拒否 | assignment_test.go:255–280、独立API試験 | PASS |
| AC-7 準備と開始 | prepared前のstartedを拒否。preparedでは準備時刻だけを設定し、続くstartedで初めてRunning・開始時刻を保存。ネイティブはstartedを送信しない | service.go:495–514、assignment_test.go:177–189、runner.rs:195–202、独立API試験 | PASS |
| INVAR-1 端末主体 | Userによるassigned-input取得を拒否。端末はinput/report/contactに限定し、User偽装・Workspace管理操作へ進めない | access/service.go:167–191、assignment_test.go:160–163、独立認可・同期試験 | PASS |
| INVAR-2 期限と実停止 | AC-5bの同じ観測と状態を使用 | 上記AC-5b、独立API試験 | PASS |
| INVAR-3 優先度更新 | 割当前に採用値P2、割当後にP1を実`automation.Put`で保存し、再読出しで採用を確認。各更新前後の全Run head一覧のID・version・payloadが完全一致し、Run数・開始/取消状態も変化なし | 独立Go overlayと独立`test:automation` | PASS |

対象Runと固定入力・Manifestの依存参照だけを読み、全Contextの走査へ追加接続していない。端末の実pathはローカル設定に保持し、共有の割当には論理ID・固定commit・領域ID・branchを保存する。Git以外のRunにrepositoryを要求していない。

## 独立試験

| コマンド | 結果 |
| --- | --- |
| `pnpm run test:runs` | PASS。TestRunsPostgres 2.22秒、package 3.623秒 |
| `pnpm run test:desktop` | PASS。通常6件成功、実機fixture用6件はignore。実Git worktree試験を含む。通常テスト1.14秒 |
| `pnpm run test:access` | PASS。TestAccessPostgres 0.88秒、package 2.067秒 |
| `pnpm run test:sync` | PASS。TestSyncPostgres 3.41秒、package 4.592秒 |
| `pnpm run test:automation` | PASS。TestAutomationPostgres 4.10秒、package 4.916秒 |
| `GOFLAGS=-overlay=... pnpm run test:runs` | PASS。INVAR-3の割当前後の実採用値変更を追加。TestRunsPostgres 2.18秒、package 3.348秒 |
| `git diff --check <基準main> <対象commit>` | PASS |

desktop試験には既存のlinker出力警告があるが、コンパイルと試験は成功した。実機fixture用のignoreを成功件数へ含めていない。専用DB試験が作成したコンテナ・volume・networkだけを片付け、標準8080/1420サービスや他のfixtureを停止していない。

INVAR-3のoverlayは`C:\Users\kgm11\AppData\Local\Temp\koujou-issue17-inspection-bf5206718811411f82f2366e6005c9e2`に保存した。既存`assignment_test.go`の検品用コピーへ実automationサービスによる採用値変更とRun一覧比較を追加し、Goの`-overlay`で読み込んだ。製品実装や既存テストの判定を置き換えていない。

## 5054f09の差分検品

初回対象の実Windowsアプリでは割当とrenew後にGit準備が失敗し、preparedを報告しなかったと実装担当から報告を受けた。APIと単体Git試験の成功だけで、この実機経路をPASSにしていない。

差分は`runner.rs`、ADR 0017、実装側証跡に限られる。作成先のrootをcanonicalizeせず、設定したapp-data pathを保持する処理を確認した。Windows prefixをGit向けに変換する既存処理は維持されている。失敗後にbranchだけが残る場合は固定commitとの一致を確認してから再利用し、一致しないbranchをresetしない。既存worktreeの検証と編集保持も維持されている。

新commitで独立に`pnpm run test:desktop`を実行し、通常6件成功・6件ignore、通常テスト1.35秒だった。二Runの分離・同じ領域への再試行・元checkout不変に加え、branchだけが残る状態からの準備も成功した。差分の`git diff --check`も成功した。API・認可・同期・優先度処理の製品ソースは変更されていないため、初回の独立試験結果を引き継ぐ。

## WebとWindowsの実操作照合

実装担当から受領した実操作画像4点を閲覧し、対応するHTTP保存記録を照合した。実装commitは5054f09で、実CLIを起動しない範囲を維持している。

| 操作と成果物 | 独立に照合した内容 | 判定 |
| --- | --- | --- |
| Windowsで同じRunを再準備。[画像](assets/issue17-native-prepared.jpg)、[準備直後の記録](assets/issue17-native-first-prepared.json) | `ui-assign-native`の準備時刻を確認。版`3MZMGDOFFRVIHLT32UNAGUTJWO`、報告位置3、queued、started_atなし。失敗でbranchだけが残った実app-data配置からの準備成功 | PASS |
| 編集後にWindowsで再準備。[記録](assets/issue17-native-first.json)、[Git証跡](assets/issue17-git-worktrees.json) | 報告位置は3から4へ進み、固定input・世代・prepared_atは同一。版`ANKRGT7S33AQEWNRIZYXFWMNLX`。実source.txtのhashが保存証跡と一致し、編集を保持 | PASS |
| 同じRunをWebで取得。[画像](assets/issue17-web-shared.png)、[記録](assets/issue17-web-shared.json) | Windows再準備後と同じ版・主体・割当・準備時刻。JSON全体もWindows側の同版記録と一致 | PASS |
| 同じrepositoryの二Run目をWindowsで準備。[画像](assets/issue17-native-second.jpg)、[記録](assets/issue17-native-second.json) | 版`W5GXSJX73AJODPIMIDQFW5YEUV`、準備時刻あり、queued、started_atなし。別の領域・branchで同じ固定commitから作成 | PASS |
| Webから別Runへ割当。[画像](assets/issue17-web-assigned.png)、[記録](assets/issue17-web-assigned.json) | 版`3Q6SY7JUTIKE6BUDQJ2NHQLKML`、Web fixture端末とRunner、CLI主体、世代・branch・commitの表示が一致。prepared_at・started_atなし、queued | PASS |

[Git証跡](assets/issue17-git-worktrees.json)が示す残存worktreeと元repositoryを独立に読み取った。二つの実HEADはともに固定commit`800cb4d51d5cb196abe3f518d25b1605dba538e5`で、branchとpathは別々だった。各source.txtのSHA-256は証跡と一致し、編集した一つ目だけが元repository・二つ目と異なる。元repositoryのHEAD・branchは保存値と一致し、working treeもcleanだった。実Git listingにはMSIX package配下のpathが記録され、準備結果には設定済みapp-data pathが返っている。

Runner接続設定は専用fixtureの所有namespaceへ準備されたものを用いる。実装担当から、UIの設定欄表示・値入力と保存済み接続の更新を操作したとの記録を受領した。「設定保存」ボタンそのものの実操作は行っておらず、その成功を本検品では主張しない。固定した末端ACは、現在の接続能力・方針を照合した割当と準備で判定した。

## 指摘と判定

- 総合判定: PASS。固定した全末端AC・INVARを満たし、Web/Windows実操作と実Gitの照合も完了した。
- コード経路と独立試験で確認したblocker: 0件。
- 未判定の末端条件: なし。未依頼のedgeや改善をblockerへ追加していない。
- CIとmerge後照合はIssueの終了手順で別に確認する。本記録だけでIssueをCloseしない。

`DESIGN.md`と`docs/adr/0014-uiux-dev-flow.md`は対象checkoutに存在しないため読めなかった。今回の検品では画面を変更せず、固定したACと実際の表示・操作を照合する。
