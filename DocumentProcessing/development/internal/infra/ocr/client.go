package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/port"
)

// HTTPAPI is a consumer-side interface covering the subset of http.Client
// methods used by this adapter. Defining it here keeps the dependency inverted
// and enables unit testing with a mock or httptest server.
type HTTPAPI interface {
	Do(req *http.Request) (*http.Response, error)
}

// Compile-time interface compliance check.
var _ port.OCRServicePort = (*Client)(nil)

// Client implements port.OCRServicePort using Yandex Cloud Vision OCR API v2.
type Client struct {
	http     HTTPAPI
	endpoint string // cfg.Endpoint + "/ocr/v1/recognizeText"
	apiKey   string
	folderID string
}

// recognizeRequest is the JSON body sent to the OCR API.
type recognizeRequest struct {
	MimeType      string   `json:"mimeType"`
	LanguageCodes []string `json:"languageCodes"`
	Content       string   `json:"content"` // base64-encoded PDF
}

// recognizeResponse is the JSON response from the OCR API.
type recognizeResponse struct {
	Result struct {
		TextAnnotation struct {
			FullText string `json:"fullText"`
		} `json:"textAnnotation"`
	} `json:"result"`
}

// NewClient creates a Client from OCRConfig with a real http.Client.
// Transport: MaxIdleConns=10, MaxIdleConnsPerHost=5, IdleConnTimeout=90s, TLSHandshakeTimeout=10s.
// Timeout: 120s.
func NewClient(cfg config.OCRConfig) *Client {
	httpClient := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	return &Client{
		http:     httpClient,
		endpoint: cfg.Endpoint + "/ocr/v1/recognizeText",
		apiKey:   cfg.APIKey,
		folderID: cfg.FolderID,
	}
}

// newClientWithHTTP creates a Client with an injected HTTPAPI (for testing).
func newClientWithHTTP(api HTTPAPI, endpoint, apiKey, folderID string) *Client {
	return &Client{
		http:     api,
		endpoint: endpoint,
		apiKey:   apiKey,
		folderID: folderID,
	}
}

// Recognize reads PDF data from pdfData, base64-encodes it, POSTs to Yandex
// Vision OCR, and returns the recognized fullText.
func (c *Client) Recognize(ctx context.Context, pdfData io.Reader) (string, error) {
	raw, err := io.ReadAll(pdfData)
	if err != nil {
		return "", mapError(err, "read input")
	}

	b64 := base64.StdEncoding.EncodeToString(raw)

	reqBody := recognizeRequest{
		MimeType:      "application/pdf",
		LanguageCodes: []string{"ru", "en"},
		Content:       b64,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", port.NewOCRError(
			fmt.Sprintf("ocr: marshal request: %v", err), false, err,
		)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return "", port.NewOCRError(
			fmt.Sprintf("ocr: create request: %v", err), false, err,
		)
	}

	httpReq.Header.Set("Authorization", "Api-Key "+c.apiKey)
	httpReq.Header.Set("x-folder-id", c.folderID)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", mapError(err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_, _ = io.Copy(io.Discard, resp.Body) // drain remainder for connection reuse
		return "", mapHTTPStatus(resp.StatusCode, string(limitedBody))
	}

	var ocrResp recognizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&ocrResp); err != nil {
		return "", port.NewOCRError(
			fmt.Sprintf("ocr: decode response: %v", err), false, err,
		)
	}

	return ocrResp.Result.TextAnnotation.FullText, nil
}
