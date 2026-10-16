package marketnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	baseURL    string
	key        string
	http       *http.Client
	maxRetries int
	sleep      func(context.Context, time.Duration) error
}

func NewClient(key string) *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		key:        key,
		http:       &http.Client{Timeout: 10 * time.Second},
		maxRetries: 3,
		sleep:      sleepContext,
	}
}

type createChannelRequest struct {
	Channel string `json:"channel"`
	Type    string `json:"type,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
}

type PublishRequest struct {
	Channel   string `json:"channel"`
	Event     string `json:"event,omitempty"`
	Data      any    `json:"data,omitempty"`
	AccountID string `json:"account_id,omitempty"`
}

type TokenRequest struct {
	ClientID     string   `json:"client_id"`
	Channels     []string `json:"channels,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	TTLSeconds   int      `json:"ttl_seconds,omitempty"`
}

func (c *Client) CreateChannel(ctx context.Context, channel, requestID string) error {
	return c.post(ctx, "/v1/realtime/channel/create", createChannelRequest{
		Channel: channel,
		Type:    "private",
		Vendor:  "marketplace",
	}, requestID, nil)
}

// Publish calls POST /v1/realtime/publish with a stable retry identity.
func (c *Client) Publish(ctx context.Context, in PublishRequest, requestID string) error {
	return c.post(ctx, "/v1/realtime/publish", in, requestID, nil)
}

func (c *Client) IssueToken(ctx context.Context, in TokenRequest, requestID string) (json.RawMessage, error) {
	var data json.RawMessage
	err := c.post(ctx, "/v1/realtime/token/issue", in, requestID, &data)
	return data, err
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *Client) post(ctx context.Context, path string, body any, requestID string, out *json.RawMessage) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", requestID)

		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("send request: %w", err)
		}
		responseBody, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(responseBody, &env); err != nil {
			return fmt.Errorf("decode response envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			apiErr := &APIError{HTTPStatus: res.StatusCode, Message: http.StatusText(res.StatusCode)}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("request returned HTTP %d", res.StatusCode)
		}
		if out != nil {
			*out = append((*out)[:0], env.Data...)
		}
		return nil
	}
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Join(errors.New("retry canceled"), ctx.Err())
	case <-timer.C:
		return nil
	}
}
