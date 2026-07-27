# Utsusemi

Apple Silicon Mac 上で動く Ephemeral セルフホスト GitHub Actions ランナー。1 ジョブ完了ごとに Tart VM を破棄します。

## 要件

- Apple Silicon Mac（macOS 15+）
- [Tart](https://tart.run/)
- GitHub Org または Repo への admin 権限
- Fine-grained PAT（PAT 直結モード）

## 最短導入（PAT 直結 / Repo）

```bash
brew tap novr/taps
brew install tart utsusemi

sudo utsusemi configure --pat \
  --repo owner/repo \
  --output /etc/utsusemi/config.yaml

utsusemi validate --config /etc/utsusemi/config.yaml
brew services start utsusemi
```

開発用（ソースから）:

```bash
go install github.com/novr/utsusemi/cmd/utsusemi@latest
# または
brew install ./Formula/utsusemi.rb
```

## Org ランナー

`runner_group_id` は GitHub の Runner group ID です。既定グループは通常 `1` です。

```bash
utsusemi configure --pat \
  --org my-org \
  --runner-group-id 1 \
  --output /etc/utsusemi/config.yaml
```

## Public App 登録

```bash
utsusemi register --broker https://broker.utsusemi.dev --repo owner/repo
brew services start utsusemi
```

## 設定

主な設定項目:

| 項目 | 説明 |
|------|------|
| `target.org` / `target.repo` | 排他。Org は `runner_group_id` 必須 |
| `labels` | `self-hosted` 必須 |
| `registration.mode` | `github_pat` / `own_app` / `hosted_app` |
| `pool_size` | 最大 2（macOS VM 制限） |

PAT / API Key / JWT は Keychain のみに保存され、設定ファイルやログには出力しません。

## 手動 E2E

1. PAT 直結 + Repo target で Agent 起動
2. `runs-on: [self-hosted, macOS]` の workflow を実行
3. ジョブ完了後に `tart list` で `utsusemi-` VM が消えていることを確認
4. Agent 再起動後、孤児 runner / VM が reconciliation で掃除されることを確認

## 開発

```bash
go test ./...
cd worker && npm install && npm test
```

## ライセンス

MIT
