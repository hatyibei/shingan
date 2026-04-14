# Shingan プレゼンスライド

Marp (Markdown Presentation Ecosystem) で作成した面接用スライド。

## ファイル構成

```
slides/
├── pitch.md              # メインスライド（面接本番用）
├── pitch-speaker-notes.md # 発表者ノート（口頭原稿）
├── theme.css             # 独自テーマ（深ネイビー + 金アクセント）
├── README.md             # このファイル
└── pdf/
    ├── pitch.pdf         # PDF出力
    └── pitch.html        # HTML出力（PDF失敗時の代替）
```

## ビルド手順

### セットアップ（初回のみ）

```bash
cd /home/hatyibei/Claude/shingan
npm init -y
npm i -D @marp-team/marp-cli
```

### PDF出力

```bash
npx marp slides/pitch.md \
  --theme-set slides/theme.css \
  --pdf \
  --allow-local-files \
  -o slides/pdf/pitch.pdf
```

### HTML出力（PDF生成できない環境での代替）

```bash
npx marp slides/pitch.md \
  --theme-set slides/theme.css \
  --html \
  --allow-local-files \
  -o slides/pdf/pitch.html
```

### ブラウザプレビュー（発表練習用）

```bash
npx marp slides/pitch.md \
  --theme-set slides/theme.css \
  --preview
```

### 全形式を一括出力

```bash
npx marp slides/pitch.md \
  --theme-set slides/theme.css \
  --pdf --html \
  --allow-local-files \
  -o slides/pdf/pitch.pdf
```

## テーマカラー

| 変数 | 値 | 用途 |
|---|---|---|
| `--color-bg` | `#0A1628` | 背景（深ネイビー） |
| `--color-gold` | `#d4af37` | アクセント（金） |
| `--color-text` | `#f0f0f0` | メインテキスト |
| `--color-text-muted` | `#b0b8c8` | サブテキスト |
| `--color-code-text` | `#4ade80` | コードブロック文字 |

## 注意事項

- `--theme-set slides/theme.css` を必ず付けること（省くとデフォルトテーマになる）
- WSL2環境でChromiumが入っていない場合、PDFは失敗する。その場合は `--html` で代替
- フォント (Noto Sans JP) はGoogle Fontsから読み込むため、初回レンダリングにネット接続が必要
