package main

import (
	"context"

	"go.lsp.dev/protocol"
)

// baseClient is a no-op implementation of every protocol.Client method,
// mirroring baseServer in baseserver.go. Tests embed this to satisfy the
// protocol.Client contract while overriding only PublishDiagnostics.
//
// This file is _test.go-only because production code never instantiates a
// Client itself — that's the editor's job. The integration test wires a
// "client" only to assert the server's outbound notifications.
type baseClient struct{}

func (baseClient) Progress(_ context.Context, _ *protocol.ProgressParams) error {
	return nil
}
func (baseClient) WorkDoneProgressCreate(_ context.Context, _ *protocol.WorkDoneProgressCreateParams) error {
	return nil
}
func (baseClient) LogMessage(_ context.Context, _ *protocol.LogMessageParams) error {
	return nil
}
func (baseClient) PublishDiagnostics(_ context.Context, _ *protocol.PublishDiagnosticsParams) error {
	return nil
}
func (baseClient) ShowMessage(_ context.Context, _ *protocol.ShowMessageParams) error {
	return nil
}
func (baseClient) ShowMessageRequest(_ context.Context, _ *protocol.ShowMessageRequestParams) (*protocol.MessageActionItem, error) {
	return nil, nil
}
func (baseClient) Telemetry(_ context.Context, _ interface{}) error {
	return nil
}
func (baseClient) RegisterCapability(_ context.Context, _ *protocol.RegistrationParams) error {
	return nil
}
func (baseClient) UnregisterCapability(_ context.Context, _ *protocol.UnregistrationParams) error {
	return nil
}
func (baseClient) ApplyEdit(_ context.Context, _ *protocol.ApplyWorkspaceEditParams) (bool, error) {
	return false, nil
}
func (baseClient) Configuration(_ context.Context, _ *protocol.ConfigurationParams) ([]interface{}, error) {
	return nil, nil
}
func (baseClient) WorkspaceFolders(_ context.Context) ([]protocol.WorkspaceFolder, error) {
	return nil, nil
}
