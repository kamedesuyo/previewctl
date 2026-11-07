# previewctl

ローカルのWebプレビューや開発サーバーを、起動から停止まで管理するAgent SkillとCLIです。
空きポートを割り当て、URL・ログ・有効期限をまとめて扱います。

## スキルとして導入

Node.js 22.20以降がある環境では、プロジェクトのディレクトリで実行します。

```sh
npx skills add kamedesuyo/previewctl --skill previewctl
```

導入先のエージェントを選択してください。
すべてのプロジェクトで使う場合は `-g`、Codexだけを選ぶ場合は `-a codex` を付けます。
導入後、スキルを再読み込みするか、エージェントを再起動してください。
Codexでは `$previewctl` で明示的に呼び出せます。

Node.jsを使わずに導入する場合は、このリポジトリをダウンロードし、`skills/previewctl` ディレクトリを、使用するエージェントのスキル格納先へコピーしてください。
`SKILL.md`・`agents`・`scripts`・`LICENSE`をまとめて配置します。
既存の同名スキルがある場合は、内容を確認してから置き換えてください。

スキルは初回利用時にOS・CPUに合うv0.1.1の実行ファイルをGitHub Releasesから取得します。
同梱のSHA-256でアーカイブと実行ファイルを検証し、次回以降はキャッシュを検証して再利用します。
管理者権限・Go・Pythonは不要で、PATHも変更しません。
WindowsではPowerShell 5.1以降、macOS・Linuxでは `sh`・`curl`・`tar` と `sha256sum` または `shasum` が必要です。
キャッシュ先を変更する場合は環境変数 `PREVIEWCTL_CACHE_DIR` を指定してください。
スキルを削除する場合は `npx skills remove previewctl` を使います。`-g` で導入した場合は削除にも `-g` を付けてください。
手動で導入した場合は、配置したスキルディレクトリを削除します。

| OS | 実行ファイルのキャッシュ先 |
| --- | --- |
| Windows | `%LOCALAPPDATA%\previewctl\cache` |
| macOS | `~/Library/Caches/previewctl` |
| Linux | `$XDG_CACHE_HOME/previewctl`、未設定なら `~/.cache/previewctl` |

キャッシュも削除する場合は、先に管理中のプレビューを停止してください。

以下はCLIを直接使う場合の操作です。

## 動作環境

| OS | 配布ファイルの識別子 |
| --- | --- |
| Windows 10以降・Windows 11 / x64 | `windows_amd64` |
| Windows 11 / ARM64 | `windows_arm64` |
| macOS 13以降 / Intel | `darwin_amd64` |
| macOS 13以降 / Apple Silicon | `darwin_arm64` |
| Linux / x64 | `linux_amd64` |
| Linux / ARM64 | `linux_arm64` |

previewctl本体にNode.js・Python・Goのインストールは不要です。
起動するアプリが使うランタイムや依存パッケージは、別途用意してください。
管理者権限は不要です。

## インストール

1. [Releases](https://github.com/kamedesuyo/previewctl/releases/latest)から、自分のOSとCPUに合うアーカイブをダウンロードします。
2. アーカイブを展開します。
3. 展開先で、以下のインストールコマンドを実行します。

WindowsのPowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

macOS・Linux:

```sh
sh ./install.sh
```

WindowsではユーザーのPATHを自動更新します。ターミナルを開き直してください。
保存先を変更する場合は `install.ps1 -InstallDir <ディレクトリ>`、PATHを変更しない場合は `-NoPathUpdate` を指定します。
macOS・Linuxでは、インストーラーが表示するPATH設定をシェルの設定ファイルに追加します。

```text
previewctl version
previewctl doctor
```

インストールせずに、展開先の実行ファイルを直接実行しても使えます。
Windowsでは `previewctl.exe`、macOS・Linuxでは `previewctl` です。
アーカイブのSHA-256は、同じリリースの `checksums.txt` で照合できます。

## 起動

```text
previewctl start --name <名前> --run '<起動コマンド>'
```

`<...>` は入力する値の位置を表します。
`--run` 全体を引用符で囲み、起動コマンドのポート指定部分に `{port}` を入れてください。
起動コマンドが環境変数 `PORT` を使う場合は、ポート指定を省略できます。

| オプション | 内容 | 既定値 |
| --- | --- | --- |
| `--name <名前>` | 管理用の名前 | 作業ディレクトリ名 |
| `--run '<起動コマンド>'` | フォアグラウンドで動くサーバーの起動コマンド | 必須 |
| `--cwd <ディレクトリ>` | 起動する場所 | 現在のディレクトリ |
| `--port <番号>` | 使用する空きポートを指定 | 自動割り当て |
| `--port-start <番号>` | 自動割り当て範囲の先頭 | `41000` |
| `--port-end <番号>` | 自動割り当て範囲の末尾 | `41999` |
| `--ttl <期間>` | 起動から停止を開始するまでの時間 | `1h` |
| `--pinned` | 時間による停止を無効化 | 無効 |
| `--wait <期間>` | ポートが接続可能になるのを待つ上限 | `10s` |
| `--grace <期間>` | macOS・Linuxで終了要求後に待つ時間 | `2s` |
| `--force` | 同じ名前・作業ディレクトリのプレビューを停止して再起動 | 無効 |
| `--json` | 結果をJSON配列で出力 | 無効 |

期間の単位は `s`（秒）・`m`（分）・`h`（時間）です。
`--grace` の範囲は `0s` から `1m` です。
WindowsではJob Objectで子プロセスを含めて停止するため、`--grace` は使いません。

| 置換文字列 | 環境変数 | 値 |
| --- | --- | --- |
| `{port}` | `PORT` | 割り当てたポート |
| `{host}` | `HOST` | `127.0.0.1` |
| `{url}` | `PREVIEW_URL` | プレビューのURL |

起動コマンドはWindowsでは `cmd.exe`、macOS・Linuxでは `/bin/sh` で実行します。
引用符や環境変数の参照には、実行先のシェルの構文を使ってください。
サーバーは割り当てられたポートと `127.0.0.1` にバインドする設定にします。
コマンド内でのバックグラウンド化や自己デーモン化は行わないでください。
previewctlがバックグラウンド実行を管理します。

起動結果にID・状態・URLが表示されます。
同じ名前・同じ作業ディレクトリ・同じ起動コマンドで再実行すると、稼働中のプレビューを返します。
既存プレビューの期限やポート設定を変更する場合は、`--force` を付けて再起動します。
同じ名前でも、作業ディレクトリが異なれば別のプレビューになります。

`running` は指定ポートへのTCP接続ができる状態です。
アプリの画面やHTTPレスポンスの正常性は、表示されたURLで確認してください。
待機上限を超えても起動が続いている場合は `starting` を返します。
コマンドが終了すると、その子プロセスも停止します。

## 一覧・ブラウザで開く

```text
previewctl list
previewctl list --all
previewctl list --json
previewctl open --id <ID>
```

通常の一覧には稼働中・停止処理中・監督プロセスへ接続できないプレビューが表示されます。
`--all` を付けると停止済みの履歴も表示します。
`open` は既定のブラウザでURLを開きます。
Linuxでは `xdg-open` が必要です。GUIのない環境では一覧のURLを利用してください。

## ログ

```text
previewctl logs --id <ID>
previewctl logs --id <ID> --tail <行数>
previewctl logs --id <ID> --json
```

標準出力・標準エラーの保存先と末尾を表示します。
既定は120行、読み取り上限は各ログの末尾1MiBです。
`--tail 0` は保存先のみを表示します。
停止後も `cleanup` するまではIDからログを取得できます。

## 停止・整理

```text
previewctl stop --id <ID>
previewctl stop --name <名前>
previewctl stop --all
previewctl cleanup
```

`--id` は1件、`--name` は同名のプレビューすべて、`--all` は全件が対象です。
通常はIDを使って停止してください。
`logs` と `open` でも `--name` が使えますが、対象が1件に定まる必要があります。

有効期限に達すると、監督プロセスが自動で停止処理を始めます。
`--pinned` を付けたプレビューは明示的に停止するまで続きます。
`cleanup` は期限切れを停止し、停止済みの登録を削除します。
ログとWindows用の起動スクリプトは保持されます。

停止は管理中の監督プロセスを通して行います。
ポート番号や記録されたPIDだけを根拠にプロセスを終了することはありません。
`unreachable` は監督プロセスへ接続できない状態です。
この状態の登録は `cleanup` で保持されるため、ログや実際のプロセスを確認したうえで、不要な登録ファイルだけを管理領域から削除してください。

## 管理領域

```text
previewctl doctor
previewctl <コマンド> --state-dir <ディレクトリ>
```

| OS | 既定の保存先 |
| --- | --- |
| Windows | `%LOCALAPPDATA%\PreviewCtl` |
| macOS | `~/Library/Application Support/previewctl` |
| Linux | `$XDG_STATE_HOME/previewctl`。未設定時は `~/.local/state/previewctl` |

`doctor` で実際の保存先・OS・CPU・シェル・空きポートを確認できます。
環境変数 `PREVIEWCTL_STATE_DIR` でも保存先を指定できます。
優先順位は `--state-dir`、環境変数、既定値の順です。
管理領域を変更した場合、一覧・停止・ログ取得にも同じ場所を指定してください。
保存先にはローカルディスク上の自分専用ディレクトリを使ってください。

`previews` に登録、`logs` にログ、`runs` にWindows用の起動スクリプトを保存します。
登録には起動コマンドと管理用の認証情報が含まれます。
管理領域を他人と共有したり、Gitに追加したりしないでください。

## 更新・アンインストール

更新前に `previewctl stop --all` を実行します。
新しいアーカイブを展開してインストーラーを再実行してください。
管理領域とログは引き継がれます。

アンインストールは `previewctl stop --all` と `previewctl cleanup` の後に行います。
Windowsでは `%LOCALAPPDATA%\Programs\previewctl\previewctl.exe` を削除し、ユーザーPATHからそのディレクトリを外してください。
macOS・Linuxでは `~/.local/bin/previewctl` を削除してください。
ログも不要なら、`doctor` で確認した管理領域を削除します。

従来のWindows PowerShell版とは管理領域が別です。
従来版で起動したプレビューは従来版で停止してください。
配布版を併用する場合は、実行ファイルのフルパスで呼び分けられます。
従来の `-Name`・`-Run`・`-Id`・`-Cwd`・`-Json`・`-TtlMinutes` 表記も受け付けます。
従来の `dashboard`・`-Expired`・`-NoOpen` は配布版に含まれません。

## ソースからインストール

Go 1.24以降を用意し、リポジトリのルートで実行します。

```sh
go install .
```

インストール先は `GOBIN`、未設定なら `GOPATH/bin` です。

```sh
go test ./...
go vet ./...
go run ./tools/package.go
```

最後のコマンドで、各OS・CPU向けのアーカイブと `checksums.txt` が `dist` に生成されます。
`-target <OS/CPU>` を付けると、動作環境表に対応する組み合わせ1種類だけを生成します。
OSには `windows`・`darwin`・`linux`、CPUには `amd64`・`arm64` を指定します。

```sh
go run ./tools/verify-release.go
```

このコマンドは現在のOS・CPU向けアーカイブを展開し、一時ディレクトリにインストールした実行ファイルで統合テストを実行します。
一時ファイルは終了時に削除され、ユーザーのPATHは変更されません。
