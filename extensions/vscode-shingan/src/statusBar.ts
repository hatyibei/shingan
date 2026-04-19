import * as vscode from 'vscode';

let item: vscode.StatusBarItem;

export function initStatusBar(ctx: vscode.ExtensionContext): void {
  item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  item.command = 'workbench.actions.view.problems';
  ctx.subscriptions.push(item);
  item.text = '$(sync~spin) Shingan';
  item.show();
}

export function setStatus(state: 'ready' | 'error' | 'analyzing'): void {
  if (!item) return;
  switch (state) {
    case 'ready':
      item.text = '$(check) Shingan';
      item.tooltip = 'Shingan LSP active';
      break;
    case 'analyzing':
      item.text = '$(sync~spin) Shingan';
      item.tooltip = 'Analyzing...';
      break;
    case 'error':
      item.text = '$(error) Shingan';
      item.tooltip = 'LSP failed; check shingan.lspPath';
      break;
  }
}
