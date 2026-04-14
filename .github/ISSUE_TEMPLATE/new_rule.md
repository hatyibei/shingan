---
name: 新ルール提案
about: 新しい解析ルールの追加を提案
title: "[Rule] "
labels: new-rule, enhancement
---

## ルールID候補
<!-- snake_case: e.g. `missing_timeout`, `unbounded_retry` -->

## 検出対象
<!-- どんな構造的バグを検出するか -->

## 典型的な誤用パターン（悪い例）
<!-- WorkflowGraph や ADK-Go コードで表現 -->

```
```

## 修正後パターン（良い例）
```
```

## Severity判定
- Critical: <!-- どんな時 -->
- Warning:
- Info:

## 実装方針（オプション）
<!-- DFS? BFS? ノード属性チェック? -->

## 関連ルール
<!-- 既存の cycle_detection, loop_guard 等との関係 -->
