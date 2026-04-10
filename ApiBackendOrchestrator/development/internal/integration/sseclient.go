package integration

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// sseEvent represents a single parsed SSE event.
type sseEvent struct {
	EventType string
	Data      string
}

// sseClient is a test helper for connecting to and reading from an SSE
// endpoint. It uses a single background goroutine to read and parse the
// SSE stream, pushing comments and events to separate channels.
type sseClient struct {
	resp     *http.Response
	comments chan string
	events   chan *sseEvent
	errs     chan error
	done     chan struct{}
}

// connectSSE opens a GET request to the SSE endpoint and returns a client
// ready to read events. The token is passed as a query parameter.
// A single reader goroutine is started to parse the SSE stream.
func connectSSE(serverURL, token string) (*sseClient, error) {
	url := serverURL + "/api/v1/events/stream?token=" + token

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connectSSE: new request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connectSSE: do: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("connectSSE: unexpected status %d", resp.StatusCode)
	}

	c := &sseClient{
		resp:     resp,
		comments: make(chan string, 64),
		events:   make(chan *sseEvent, 64),
		errs:     make(chan error, 1),
		done:     make(chan struct{}),
	}

	// Start a single goroutine that reads and parses the entire SSE stream.
	go c.readLoop(bufio.NewReader(resp.Body))
	return c, nil
}

// readLoop runs in a background goroutine. It reads lines from the SSE stream
// and dispatches parsed comments and events to their respective channels.
func (c *sseClient) readLoop(reader *bufio.Reader) {
	defer close(c.done)

	var eventType string
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			select {
			case c.errs <- err:
			default:
			}
			return
		}

		trimmed := strings.TrimRight(line, "\r\n")

		// Empty line = end of SSE event frame.
		if trimmed == "" {
			if eventType != "" || len(dataLines) > 0 {
				evt := &sseEvent{
					EventType: eventType,
					Data:      strings.Join(dataLines, "\n"),
				}
				select {
				case c.events <- evt:
				default:
				}
				eventType = ""
				dataLines = nil
			}
			continue
		}

		// Comment line (starts with ':'). Push to comments channel.
		if strings.HasPrefix(trimmed, ":") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, ":"))
			select {
			case c.comments <- text:
			default:
			}
			continue
		}

		// Parse "event: TYPE" line.
		if strings.HasPrefix(trimmed, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			continue
		}

		// Parse "data: JSON" line.
		if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			dataLines = append(dataLines, data)
			continue
		}
	}
}

// WaitForConnected reads from the comments channel until "connected" is found,
// or until the timeout expires.
func (c *sseClient) WaitForConnected(timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case comment := <-c.comments:
			if comment == "connected" {
				return nil
			}
		case err := <-c.errs:
			return fmt.Errorf("WaitForConnected: read error: %w", err)
		case <-deadline:
			return fmt.Errorf("WaitForConnected: timeout after %v", timeout)
		}
	}
}

// NextEvent reads the next SSE event from the stream. Returns the parsed
// event or an error if the timeout expires.
func (c *sseClient) NextEvent(timeout time.Duration) (*sseEvent, error) {
	deadline := time.After(timeout)
	select {
	case evt := <-c.events:
		return evt, nil
	case err := <-c.errs:
		return nil, fmt.Errorf("NextEvent: read error: %w", err)
	case <-deadline:
		return nil, fmt.Errorf("NextEvent: timeout after %v", timeout)
	}
}

// Close closes the SSE connection by closing the response body.
func (c *sseClient) Close() {
	if c.resp != nil && c.resp.Body != nil {
		c.resp.Body.Close()
	}
	// Wait for readLoop to finish to avoid leaking goroutines.
	<-c.done
}
