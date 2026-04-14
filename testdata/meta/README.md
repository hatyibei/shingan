# testdata/meta — Self-Dogfood fixtures

## `shingan_pipeline.json`

Shinganの解析パイプライン (ParserFactory → AnalysisOrchestrator → ReporterFactory) を WorkflowGraph として表現したメタサンプル。

これに対して `shingan analyze` を実行すると、現行ルールの誤検知が5件発火する。
詳細は [docs/self-dogfood.md](../../docs/self-dogfood.md)。

このfixtureはSelf-dogfoodingおよびv0.2での誤検知率改善の回帰テスト用。

```bash
./shingan analyze --format json --input testdata/meta/shingan_pipeline.json --output markdown
# Total: 5 | Critical: 2 | Warning: 0 | Info: 3 (現状すべて既知の誤検知)
```
