import * as vscode from 'vscode';
import { LanguageClient } from 'vscode-languageclient/node';

export function registerCommands(
  ctx: vscode.ExtensionContext,
  _client: LanguageClient
): void {
  ctx.subscriptions.push(
    vscode.commands.registerCommand('shingan.analyzeCurrentFile', async () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        return;
      }
      // LSP の didSave を trigger するだけで解析走る (shingan.analyzeOnSave=true前提)
      await editor.document.save();
    }),
    vscode.commands.registerCommand('shingan.analyzeWorkspace', async () => {
      const files = await vscode.workspace.findFiles('**/*.{go,json}');
      for (const f of files) {
        const doc = await vscode.workspace.openTextDocument(f);
        await doc.save();
      }
      vscode.window.showInformationMessage(
        `Shingan: requested analysis for ${files.length} files`
      );
    }),
    vscode.commands.registerCommand('shingan.showRules', () => {
      vscode.env.openExternal(
        vscode.Uri.parse('https://github.com/hatyibei/shingan#解析ルール一覧')
      );
    })
  );
}
