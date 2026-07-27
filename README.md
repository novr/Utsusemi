# Utsusemi

Apple Silicon Mac 上で動く Ephemeral セルフホスト GitHub Actions ランナー。1 ジョブ完了ごとに Tart VM を破棄します。

## 要件

- Apple Silicon Mac（macOS 15+）
- GitHub Org または Repo への admin 権限
- Fine-grained PAT（PAT 直結モード）
- VM Provider に応じたホスト依存（例: `provider: tart` なら [Tart](https://tart.run/)）

## 導入（PAT 直結 / Repo）

```bash
brew tap novr/taps
brew install utsusemi
brew install tart   # provider: tart の場合

sudo utsusemi configure --pat \
  --repo owner/repo \
  --output /etc/utsusemi/config.yaml

utsusemi validate --config /etc/utsusemi/config.yaml
brew services start utsusemi
```

## Org ランナー

```bash
utsusemi configure --pat \
  --org my-org \
  --runner-group-id 1 \
  --output /etc/utsusemi/config.yaml
```

## Public App 登録

```bash
utsusemi register --broker https://broker.utsusemi.dev \
  --client-id Iv1.YOUR_APP_CLIENT_ID \
  --repo owner/repo
brew services start utsusemi
```

設定例は [examples/config.pat.yaml](examples/config.pat.yaml)。認証情報は Keychain のみに保存します。

## 開発

```bash
make test
make build
```

## ライセンス

MIT
