import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind
} from 'vscode-languageclient/node';
import { initStatusBar, setStatus } from './statusBar';
import { registerCommands } from './commands';

let client: LanguageClient | undefined;

export async function activate(ctx: vscode.ExtensionContext): Promise<void> {
  const cfg = vscode.workspace.getConfiguration('shingan');
  const command = cfg.get<string>('lspPath', 'shingan-lsp');

  const serverOptions: ServerOptions = {
    run: { command, transport: TransportKind.stdio },
    debug: { command, transport: TransportKind.stdio }
  };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      { scheme: 'file', language: 'go' },
      { scheme: 'file', language: 'json' }
    ],
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher('**/*.{go,json}')
    }
  };

  client = new LanguageClient('shingan', 'Shingan LSP', serverOptions, clientOptions);
  initStatusBar(ctx);
  registerCommands(ctx, client);

  try {
    await client.start();
    setStatus('ready');
  } catch (e) {
    vscode.window.showErrorMessage(
      `Shingan LSP failed to start: ${(e as Error).message}. Check shingan.lspPath setting.`
    );
    setStatus('error');
  }
}

export async function deactivate(): Promise<void> {
  if (client) {
    await client.stop();
  }
}
