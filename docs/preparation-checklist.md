# 面接当日チェックリスト (2026-04-17 木)

## 起床〜出発 (2時間前)
- [ ] 起きたら水、コーヒー
- [ ] シャワー
- [ ] スーツ/ビジネスカジュアル確認
- [ ] PC持参、充電100%、電源ケーブル
- [ ] 名刺 or 自己紹介資料（印刷）

## PC環境確認 (1時間前)
- [ ] git pull origin main
- [ ] gcloud auth application-default login  （ADC 12h有効期限）
- [ ] export GOOGLE_CLOUD_PROJECT=axial-mercury-486503-j5
- [ ] export GOOGLE_CLOUD_LOCATION=us-central1
- [ ] export GOOGLE_GENAI_USE_VERTEXAI=true
- [ ] go build -o shingan ./cmd/shingan
- [ ] go build -o shingan-runner ./cmd/runner
- [ ] go build -o shingan-web ./cmd/shingan-web
- [ ] ./shingan-web & でブラウザ localhost:8080 動作確認（3agent表示）
- [ ] ./shingan-runner --sample simple で Gemini応答確認
- [ ] pkill -f shingan-web

## 面接会場 (30分前)
- [ ] 現地到着、トイレ、深呼吸
- [ ] スマホでinterview-cheatsheet.md最終確認
- [ ] 30秒ピッチを口ずさむ

## 面接直前 (5分前)
- [ ] PCをテザリング用意 (Wi-Fi が無い場合)
- [ ] ./shingan-web を起動してブラウザで表示待機
- [ ] 画面共有ツールを確認（Zoom/Meet）

## デモ中の気をつけるポイント
- Step1の結果は10ms で出るので、画面を指差して説明しながら15-20秒尺を稼ぐ
- Vertex AI応答は2秒程度だが、コードの構造を指差して時間を埋める
- 想定外のエラーが出たら dry-run に切り替える ("--dry-run")

## もしもトラブル対処

| 症状 | 対処 |
|---|---|
| ADC切れ | gcloud auth application-default login 再実行 |
| Vertex AI遅延 | scripts/screenshots/ のPNG画像で代替説明 |
| shingan-web起動失敗 | shingan CLI で同等デモに切替 |
| Wi-Fi無い | テザリングに切替 |
| 時間切れ30秒ピッチだけで終わる可能性 | 3分ピッチは省略、コード見せ5分に集中 |
