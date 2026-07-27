# Homebrew tap 公開

[novr/homebrew-taps](https://github.com/novr/homebrew-taps) 経由で配布する。

## インストール

```bash
brew tap novr/taps
brew install tart utsusemi
```

## 初回リリース手順

1. `novr/utsusemi` リポジトリを GitHub に作成し push する
2. リポジトリ Secrets に `NOVRD_BOT_CLIENT_ID` / `NOVRD_BOT_KEY` を設定する
3. Org Settings → Actions → General で、`novr/utsusemi` から `novr/homebrew-taps` の reusable workflow 呼び出しを許可する
4. `homebrew-taps` に `Formula/utsusemi.rb` が入っていることを確認する（初回は手動追加、以降は dispatch で url/sha256/version のみ更新）
5. `v0.1.0` タグを push する

```bash
git tag v0.1.0
git push origin v0.1.0
```

6. Release workflow が macOS universal binary を公開し、`update-formula` dispatch で tap を更新する
7. 確認:

```bash
brew update
brew install utsusemi
utsusemi --help
```

## アセット命名

```
utsusemi_<version>_darwin.tar.gz
```

例: `utsusemi_0.1.0_darwin.tar.gz`

## ローカル Formula

リポジトリ内の [`Formula/utsusemi.rb`](../Formula/utsusemi.rb) はソースからのローカル開発用。配布は tap を正とする。

## 手動で tap を更新する場合

```bash
gh api repos/novr/homebrew-taps/dispatches --method POST --input - <<EOF
{
  "event_type": "update-formula",
  "client_payload": {
    "formula": "utsusemi",
    "version": "0.1.0",
    "url": "https://github.com/novr/utsusemi/releases/download/v0.1.0/utsusemi_0.1.0_darwin.tar.gz",
    "sha256": "<sha256>",
    "desc": "Ephemeral self-hosted GitHub Actions runners on Apple Silicon Macs",
    "homepage": "https://github.com/novr/utsusemi",
    "source_repo": "novr/utsusemi",
    "binary": "utsusemi",
    "test_match": "Ephemeral"
  }
}
EOF
```

`depends_on "tart"` や `service` ブロックは `Formula/utsusemi.rb` に手動で維持する。dispatch は `version` / `url` / `sha256` のみ更新する。
