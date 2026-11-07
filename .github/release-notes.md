Windows・macOS・Linux向けのAgent Skillと単体実行ファイルです。

スキルとして導入する場合は、プロジェクトのディレクトリで実行し、導入先のエージェントを選択してください。

```sh
npx skills add kamedesuyo/previewctl --skill previewctl
```

スキルは初回利用時に対応する実行ファイルを取得し、同梱のSHA-256で検証します。
手動で導入する場合は、ソース内の `skills/previewctl` ディレクトリをまとめてコピーしてください。

CLIを直接使う場合:

1. OSとCPUに合うアーカイブをダウンロードして展開してください。
2. Windowsは `install.ps1`、macOS・Linuxは `install.sh` でインストールします。
3. 導入手順と使い方は同梱の `README.md` を参照してください。

`checksums.txt` に各アーカイブのSHA-256を収録しています。

Windows x64とLinux x64は、配布物をインストールした状態で7項目の統合テストを確認しています。
macOS版とARM64版は、OS別コードの確認・静的検査・クロスビルドによる確認です。実機では未検証です。

GitHub Actionsの6環境での検証は、必要なときに手動実行できます。
