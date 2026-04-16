package app

import (
	"context"

	"contractpro/api-orchestrator/internal/application/confirmtype"
	"contractpro/api-orchestrator/internal/application/upload"
	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/broker"
	"contractpro/api-orchestrator/internal/ingress/consumer"
)

// Compile-time interface checks.
var (
	_ consumer.BrokerSubscriber       = (*brokerSubscriberAdapter)(nil)
	_ upload.DMClient                 = (*uploadDMAdapter)(nil)
	_ upload.CommandPublisher         = (*uploadCmdPubAdapter)(nil)
	_ confirmtype.CommandPublisher    = (*confirmTypeCmdPubAdapter)(nil)
)

// ---------------------------------------------------------------------------
// brokerSubscriberAdapter bridges *broker.Client to consumer.BrokerSubscriber.
//
// Required because broker.Client.Subscribe takes the named type
// broker.MessageHandler, while consumer.BrokerSubscriber declares an
// equivalent but unnamed func(context.Context, []byte) error parameter.
// Go's type system does not unify named and unnamed function types in
// interface satisfaction checks.
// ---------------------------------------------------------------------------

type brokerSubscriberAdapter struct {
	client *broker.Client
}

func (a *brokerSubscriberAdapter) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	return a.client.Subscribe(topic, handler)
}

// ---------------------------------------------------------------------------
// uploadDMAdapter bridges *dmclient.Client to upload.DMClient.
//
// The upload package defines its own DTO types (CreateDocumentRequest,
// Document, etc.) to keep the application layer decoupled from the egress
// layer. This adapter converts between the two type sets.
// ---------------------------------------------------------------------------

type uploadDMAdapter struct {
	client *dmclient.Client
}

func (a *uploadDMAdapter) CreateDocument(ctx context.Context, req upload.CreateDocumentRequest) (*upload.Document, error) {
	doc, err := a.client.CreateDocument(ctx, dmclient.CreateDocumentRequest{
		Title: req.Title,
	})
	if err != nil {
		return nil, err
	}
	return &upload.Document{DocumentID: doc.DocumentID}, nil
}

func (a *uploadDMAdapter) CreateVersion(ctx context.Context, documentID string, req upload.CreateVersionRequest) (*upload.DocumentVersion, error) {
	ver, err := a.client.CreateVersion(ctx, documentID, dmclient.CreateVersionRequest{
		SourceFileKey:      req.SourceFileKey,
		SourceFileName:     req.SourceFileName,
		SourceFileSize:     req.SourceFileSize,
		SourceFileChecksum: req.SourceFileChecksum,
		OriginType:         req.OriginType,
	})
	if err != nil {
		return nil, err
	}
	return &upload.DocumentVersion{
		VersionID:     ver.VersionID,
		VersionNumber: ver.VersionNumber,
	}, nil
}

// ---------------------------------------------------------------------------
// uploadCmdPubAdapter bridges *commandpub.Publisher to upload.CommandPublisher.
//
// The upload package defines its own ProcessDocumentCommand type to avoid
// depending on the commandpub package directly.
// ---------------------------------------------------------------------------

type uploadCmdPubAdapter struct {
	pub *commandpub.Publisher
}

func (a *uploadCmdPubAdapter) PublishProcessDocument(ctx context.Context, cmd upload.ProcessDocumentCommand) error {
	return a.pub.PublishProcessDocument(ctx, commandpub.ProcessDocumentCommand{
		JobID:              cmd.JobID,
		DocumentID:         cmd.DocumentID,
		VersionID:          cmd.VersionID,
		OrganizationID:     cmd.OrganizationID,
		RequestedByUserID:  cmd.RequestedByUserID,
		SourceFileKey:      cmd.SourceFileKey,
		SourceFileName:     cmd.SourceFileName,
		SourceFileSize:     cmd.SourceFileSize,
		SourceFileChecksum: cmd.SourceFileChecksum,
		SourceFileMIMEType: cmd.SourceFileMIMEType,
	})
}

// ---------------------------------------------------------------------------
// confirmTypeCmdPubAdapter bridges *commandpub.Publisher to
// confirmtype.CommandPublisher.
// ---------------------------------------------------------------------------

type confirmTypeCmdPubAdapter struct {
	pub *commandpub.Publisher
}

func (a *confirmTypeCmdPubAdapter) PublishUserConfirmedType(ctx context.Context, cmd confirmtype.UserConfirmedTypeCommand) error {
	return a.pub.PublishUserConfirmedType(ctx, commandpub.UserConfirmedTypeCommand{
		JobID:             cmd.JobID,
		DocumentID:        cmd.DocumentID,
		VersionID:         cmd.VersionID,
		OrganizationID:    cmd.OrganizationID,
		ConfirmedByUserID: cmd.ConfirmedByUserID,
		ContractType:      cmd.ContractType,
	})
}
