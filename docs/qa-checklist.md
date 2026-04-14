# 面接直前 最終QAチェックリスト (2026-04-17 朝)

> 起床後、家を出る前に上から順にチェック。1項目でもNGなら bash scripts/demo.sh --dry-run に切り替える。

## 1. Git 状態

- [ ] `cd /home/hatyibei/Claude/shingan`
- [ ] `git status` → `working tree clean`
- [ ] `git pull origin main`
- [ ] `git log --oneline -5` → 面接想定のlatest commitが見える

## 2. 認証

- [ ] `gcloud config get-value project` → `axial-mercury-486503-j5` or default可
- [ ] `gcloud auth application-default print-access-token | head -c 20` → トークン先頭20字出力
- [ ] 出力されなかったら `gcloud auth application-default login` 再実行（インタラクティブ、ブラウザ要）

## 3. 環境変数

```bash
export GOOGLE_CLOUD_PROJECT=axial-mercury-486503-j5
export GOOGLE_CLOUD_LOCATION=us-central1
export GOOGLE_GENAI_USE_VERTEXAI=true
```

確認:
- [ ] `echo $GOOGLE_CLOUD_PROJECT` → `axial-mercury-486503-j5`

## 4. バイナリビルド

- [ ] `go build -o shingan ./cmd/shingan`
- [ ] `go build -o shingan-runner ./cmd/runner`
- [ ] `go build -o shingan-web ./cmd/shingan-web`
- [ ] `ls -la shingan shingan-runner shingan-web` → 全部 ≥ 3MB

## 5. テスト

- [ ] `go vet ./...` → 出力なし
- [ ] `go test -race ./...` → 全パッケージOK

## 6. CLI動作確認

- [ ] `./shingan analyze --format adk-go --input examples/runtime/infinite_loop_unbounded.go --output markdown` → Critical + Warning 2件以上
- [ ] `./shingan-runner --sample infinite_loop_unbounded --dry-run` → EXECUTION REFUSED
- [ ] `./shingan-runner --sample simple` → `こんにちは！` 応答（Vertex AI実行）

## 7. Web UI動作確認

- [ ] `./shingan-web &` でバックグラウンド起動
- [ ] 3秒待ってから `curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/` → `302` or `200`
- [ ] ブラウザで `http://localhost:8080` → 3 agent一覧表示
- [ ] `infinite_loop_unbounded` 選択 → チャットに "hi" 送信 → エラー表示
- [ ] `simple_hello` 選択 → チャットに "hi" 送信 → Gemini応答
- [ ] `pkill -f "./shingan-web"` で終了

## 8. ネットワーク

- [ ] Wi-Fi OK
- [ ] ダメならテザリング設定済み、切替テスト済

## 9. プレゼン資料

- [ ] `docs/interview-cheatsheet.md` をスマホで開ける
- [ ] `docs/demo-transcript.md` をスマホで開ける
- [ ] `docs/reverse-questions.md` を印刷 or スマホで開ける
- [ ] (あれば) `slides/pdf/pitch.pdf` がPC上にある

## 10. 持ち物

- [ ] PC + 電源
- [ ] モバイルバッテリー
- [ ] イヤホン（オンライン面接用）
- [ ] 身分証明書
- [ ] 飲み水

## NG時のフォールバック

| NGの項目 | フォールバック |
|---|---|
| ADC切れ & 再認証不可 | `--dry-run` のみで進行 |
| Vertex AI 失敗 | scripts/screenshots/ のPNGで代替説明 |
| shingan-web 起動失敗 | CLI 3コマンド (analyze + runner dry-run + runner simple) のみ |
| ネット完全断 | CLI解析のみ、Geminiデモは口頭+スクショ |
| PC故障 | 面接官に状況説明、スライドPDFを印刷物として持参（印刷しておく） |

## 面接開始10分前の最終儀式

1. 深呼吸3回
2. `docs/interview-cheatsheet.md` 冒頭の30秒ピッチを黙読
3. 姿勢を正す
4. 笑顔
