package validator

import (
	"context"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

const (
	testMaxFileSize     = int64(20 * 1024 * 1024) // 20 MB
	testAllowedMimeType = "application/pdf"
)

func validCommand() model.ProcessDocumentCommand {
	return model.ProcessDocumentCommand{
		JobID:      "job-1",
		DocumentID: "doc-1",
		FileURL:    "https://storage.example.com/file.pdf",
		FileSize:   1024,
		MimeType:   "application/pdf",
	}
}

func TestValidate(t *testing.T) {
	v := NewValidator(testMaxFileSize, testAllowedMimeType)
	ctx := context.Background()

	tests := []struct {
		name         string
		modify       func(*model.ProcessDocumentCommand)
		wantErr      bool
		wantErrCode  string
	}{
		{
			name:    "valid command with all fields",
			modify:  func(cmd *model.ProcessDocumentCommand) {},
			wantErr: false,
		},
		{
			name:        "empty document_id",
			modify:      func(cmd *model.ProcessDocumentCommand) { cmd.DocumentID = "" },
			wantErr:     true,
			wantErrCode: port.ErrCodeValidation,
		},
		{
			name:        "empty file_url",
			modify:      func(cmd *model.ProcessDocumentCommand) { cmd.FileURL = "" },
			wantErr:     true,
			wantErrCode: port.ErrCodeValidation,
		},
		{
			name: "file_size exceeds limit",
			modify: func(cmd *model.ProcessDocumentCommand) {
				cmd.FileSize = testMaxFileSize + 1
			},
			wantErr:     true,
			wantErrCode: port.ErrCodeFileTooLarge,
		},
		{
			name: "file_size exactly at limit",
			modify: func(cmd *model.ProcessDocumentCommand) {
				cmd.FileSize = testMaxFileSize
			},
			wantErr: false,
		},
		{
			name: "file_size zero — not declared, skip check",
			modify: func(cmd *model.ProcessDocumentCommand) {
				cmd.FileSize = 0
			},
			wantErr: false,
		},
		{
			name: "invalid mime_type",
			modify: func(cmd *model.ProcessDocumentCommand) {
				cmd.MimeType = "text/plain"
			},
			wantErr:     true,
			wantErrCode: port.ErrCodeInvalidFormat,
		},
		{
			name: "mime_type empty — not declared, skip check",
			modify: func(cmd *model.ProcessDocumentCommand) {
				cmd.MimeType = ""
			},
			wantErr: false,
		},
		{
			name: "mime_type application/pdf passes",
			modify: func(cmd *model.ProcessDocumentCommand) {
				cmd.MimeType = "application/pdf"
			},
			wantErr: false,
		},
		{
			name: "document_id empty takes priority over file_url empty",
			modify: func(cmd *model.ProcessDocumentCommand) {
				cmd.DocumentID = ""
				cmd.FileURL = ""
			},
			wantErr:     true,
			wantErrCode: port.ErrCodeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := validCommand()
			tt.modify(&cmd)

			err := v.Validate(ctx, cmd)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				code := port.ErrorCode(err)
				if code != tt.wantErrCode {
					t.Errorf("expected error code %q, got %q", tt.wantErrCode, code)
				}
				if !port.IsDomainError(err) {
					t.Error("expected DomainError")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}
