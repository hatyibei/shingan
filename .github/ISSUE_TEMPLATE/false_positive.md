---
name: False positive report
about: shingan が flag した finding が実際は問題ない場合のレポート (Zero-FP guarantee 対象)
title: "[FP] <rule_name>: <repo or pattern>"
labels: false-positive
---

## Zero-FP guarantee について

Shingan は **Critical false positive を抱えない** ことを最大資産として運用しています。Critical FP がここで報告された場合:

- **24h 以内に triage** (best-effort, 平日)
- **次の release で fix + 回帰 testdata 固定** が原則
- 修正 commit には CHANGELOG に `dogfood-driven` タグ付き entry を残します

Warning / Info の FP は同じテンプレで OK ですが、SLA は緩めです (1 週間以内に triage)。

## 報告するもの (どれか 1 つ以上)

- 公開 OSS の URL + commit / tag (best for repro)
- 最小再現の Python / JSON ファイル (添付 or gist)
- shingan の出力 (`--output=markdown` のセクション)

---

## Repo / file under analysis

<!-- e.g. https://github.com/<owner>/<repo>/blob/<sha>/path/to/file.py
     or 最小再現コード -->

## Shingan command

```bash
shingan analyze --format=langgraph --input=<...> --output=markdown --min-confidence=0.7
```

## Finding がどう出たか (paste)

<!-- shingan の markdown 出力をそのまま貼る -->

## なぜ FP と判断したか

<!-- 実際のグラフ構造 / 意図された設計 / runtime での挙動 など -->

## (任意) 期待する fix の方向

<!-- 「parser が <X> idiom を unroll すべき」「rule の confidence を下げるべき」など -->

## 環境

- shingan version: (`shingan version`)
- Framework + version: (e.g. `langgraph==0.2.50`)
- OS:
