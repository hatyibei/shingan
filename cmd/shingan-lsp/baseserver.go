package main

import (
	"context"

	"go.lsp.dev/protocol"
)

// baseServer is a no-op implementation of every protocol.Server method.
//
// go.lsp.dev/protocol does not ship an UnimplementedServer-style helper, but
// the dispatcher (server.go in the upstream package) walks every method
// statically — every request that lands at the dispatch switch ends up
// invoking one of these methods regardless of whether we want to handle it.
//
// Embedding baseServer in the real Server lets us override only the small
// set of methods that drive Shingan's diagnostics flow (Initialize,
// Initialized, Shutdown, Exit, DidOpen, DidChange, DidClose, Hover,
// CodeAction) without having to maintain a hand-written stub for the
// remaining ~60 methods. As LSP gains capabilities (semantic tokens, code
// lens, etc.) we override the relevant methods one at a time on the
// concrete Server, leaving the unrelated ones inert.
//
// Methods that return a result use the type's zero value (nil for slices /
// pointers, false for bool, "" for string), which the LSP client interprets
// as "feature not supported" once the server's ServerCapabilities indicate
// non-support. Methods that only return an error return nil — silently
// ignoring notifications we don't care about is the standard LSP behaviour.
type baseServer struct{}

// --- Lifecycle ---

func (baseServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{}, nil
}
func (baseServer) Initialized(_ context.Context, _ *protocol.InitializedParams) error { return nil }
func (baseServer) Shutdown(_ context.Context) error                                   { return nil }
func (baseServer) Exit(_ context.Context) error                                       { return nil }
func (baseServer) WorkDoneProgressCancel(_ context.Context, _ *protocol.WorkDoneProgressCancelParams) error {
	return nil
}
func (baseServer) LogTrace(_ context.Context, _ *protocol.LogTraceParams) error { return nil }
func (baseServer) SetTrace(_ context.Context, _ *protocol.SetTraceParams) error { return nil }

// --- Editor features (we only override the ones we use) ---

func (baseServer) CodeAction(_ context.Context, _ *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	return nil, nil
}
func (baseServer) CodeLens(_ context.Context, _ *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	return nil, nil
}
func (baseServer) CodeLensResolve(_ context.Context, lens *protocol.CodeLens) (*protocol.CodeLens, error) {
	return lens, nil
}
func (baseServer) ColorPresentation(_ context.Context, _ *protocol.ColorPresentationParams) ([]protocol.ColorPresentation, error) {
	return nil, nil
}
func (baseServer) Completion(_ context.Context, _ *protocol.CompletionParams) (*protocol.CompletionList, error) {
	return nil, nil
}
func (baseServer) CompletionResolve(_ context.Context, item *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	return item, nil
}
func (baseServer) Declaration(_ context.Context, _ *protocol.DeclarationParams) ([]protocol.Location, error) {
	return nil, nil
}
func (baseServer) Definition(_ context.Context, _ *protocol.DefinitionParams) ([]protocol.Location, error) {
	return nil, nil
}

// --- Sync notifications ---

func (baseServer) DidChange(_ context.Context, _ *protocol.DidChangeTextDocumentParams) error {
	return nil
}
func (baseServer) DidChangeConfiguration(_ context.Context, _ *protocol.DidChangeConfigurationParams) error {
	return nil
}
func (baseServer) DidChangeWatchedFiles(_ context.Context, _ *protocol.DidChangeWatchedFilesParams) error {
	return nil
}
func (baseServer) DidChangeWorkspaceFolders(_ context.Context, _ *protocol.DidChangeWorkspaceFoldersParams) error {
	return nil
}
func (baseServer) DidClose(_ context.Context, _ *protocol.DidCloseTextDocumentParams) error {
	return nil
}
func (baseServer) DidOpen(_ context.Context, _ *protocol.DidOpenTextDocumentParams) error {
	return nil
}
func (baseServer) DidSave(_ context.Context, _ *protocol.DidSaveTextDocumentParams) error {
	return nil
}

// --- Document features ---

func (baseServer) DocumentColor(_ context.Context, _ *protocol.DocumentColorParams) ([]protocol.ColorInformation, error) {
	return nil, nil
}
func (baseServer) DocumentHighlight(_ context.Context, _ *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	return nil, nil
}
func (baseServer) DocumentLink(_ context.Context, _ *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	return nil, nil
}
func (baseServer) DocumentLinkResolve(_ context.Context, link *protocol.DocumentLink) (*protocol.DocumentLink, error) {
	return link, nil
}
func (baseServer) DocumentSymbol(_ context.Context, _ *protocol.DocumentSymbolParams) ([]interface{}, error) {
	return nil, nil
}
func (baseServer) ExecuteCommand(_ context.Context, _ *protocol.ExecuteCommandParams) (interface{}, error) {
	return nil, nil
}
func (baseServer) FoldingRanges(_ context.Context, _ *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	return nil, nil
}
func (baseServer) Formatting(_ context.Context, _ *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	return nil, nil
}
func (baseServer) Hover(_ context.Context, _ *protocol.HoverParams) (*protocol.Hover, error) {
	return nil, nil
}
func (baseServer) Implementation(_ context.Context, _ *protocol.ImplementationParams) ([]protocol.Location, error) {
	return nil, nil
}
func (baseServer) OnTypeFormatting(_ context.Context, _ *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	return nil, nil
}
func (baseServer) PrepareRename(_ context.Context, _ *protocol.PrepareRenameParams) (*protocol.Range, error) {
	return nil, nil
}
func (baseServer) RangeFormatting(_ context.Context, _ *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	return nil, nil
}
func (baseServer) References(_ context.Context, _ *protocol.ReferenceParams) ([]protocol.Location, error) {
	return nil, nil
}
func (baseServer) Rename(_ context.Context, _ *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	return nil, nil
}
func (baseServer) SignatureHelp(_ context.Context, _ *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	return nil, nil
}
func (baseServer) Symbols(_ context.Context, _ *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	return nil, nil
}
func (baseServer) TypeDefinition(_ context.Context, _ *protocol.TypeDefinitionParams) ([]protocol.Location, error) {
	return nil, nil
}
func (baseServer) WillSave(_ context.Context, _ *protocol.WillSaveTextDocumentParams) error {
	return nil
}
func (baseServer) WillSaveWaitUntil(_ context.Context, _ *protocol.WillSaveTextDocumentParams) ([]protocol.TextEdit, error) {
	return nil, nil
}
func (baseServer) ShowDocument(_ context.Context, _ *protocol.ShowDocumentParams) (*protocol.ShowDocumentResult, error) {
	return nil, nil
}
func (baseServer) WillCreateFiles(_ context.Context, _ *protocol.CreateFilesParams) (*protocol.WorkspaceEdit, error) {
	return nil, nil
}
func (baseServer) DidCreateFiles(_ context.Context, _ *protocol.CreateFilesParams) error { return nil }
func (baseServer) WillRenameFiles(_ context.Context, _ *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	return nil, nil
}
func (baseServer) DidRenameFiles(_ context.Context, _ *protocol.RenameFilesParams) error { return nil }
func (baseServer) WillDeleteFiles(_ context.Context, _ *protocol.DeleteFilesParams) (*protocol.WorkspaceEdit, error) {
	return nil, nil
}
func (baseServer) DidDeleteFiles(_ context.Context, _ *protocol.DeleteFilesParams) error { return nil }
func (baseServer) CodeLensRefresh(_ context.Context) error                               { return nil }
func (baseServer) PrepareCallHierarchy(_ context.Context, _ *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	return nil, nil
}
func (baseServer) IncomingCalls(_ context.Context, _ *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	return nil, nil
}
func (baseServer) OutgoingCalls(_ context.Context, _ *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	return nil, nil
}
func (baseServer) SemanticTokensFull(_ context.Context, _ *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	return nil, nil
}
func (baseServer) SemanticTokensFullDelta(_ context.Context, _ *protocol.SemanticTokensDeltaParams) (interface{}, error) {
	return nil, nil
}
func (baseServer) SemanticTokensRange(_ context.Context, _ *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	return nil, nil
}
func (baseServer) SemanticTokensRefresh(_ context.Context) error { return nil }
func (baseServer) LinkedEditingRange(_ context.Context, _ *protocol.LinkedEditingRangeParams) (*protocol.LinkedEditingRanges, error) {
	return nil, nil
}
func (baseServer) Moniker(_ context.Context, _ *protocol.MonikerParams) ([]protocol.Moniker, error) {
	return nil, nil
}
func (baseServer) Request(_ context.Context, _ string, _ interface{}) (interface{}, error) {
	return nil, nil
}
