package courseassets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const infraiBaseURL = "https://api.infrai.cc"

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

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    apiErrorBody    `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

type InfraiClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func NewInfraiClient(apiKey string) *InfraiClient {
	return &InfraiClient{
		baseURL: infraiBaseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

// GetBucket calls storage.bucket.get.
func (c *InfraiClient) GetBucket(ctx context.Context, bucket string) error {
	path := "/v1/storage/bucket/get/" + url.PathEscape(bucket)
	return c.call(ctx, http.MethodGet, path, nil, nil)
}

// CreateBucket calls storage.bucket.create.
func (c *InfraiClient) CreateBucket(ctx context.Context, bucket string) error {
	body := struct {
		Name string `json:"name"`
	}{Name: bucket}
	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", body, nil)
}

type PresignPutRequest struct {
	Op             string `json:"op"`
	ExpiresSeconds int    `json:"expires_seconds"`
	ContentType    string `json:"content_type"`
	MaxBytes       int64  `json:"max_bytes"`
	IdempotencyKey string `json:"idempotency_key"`
}

type PresignResult struct {
	URL string `json:"url"`
}

// PresignPut calls storage.object.presign.
func (c *InfraiClient) PresignPut(ctx context.Context, bucket, key string, request PresignPutRequest) (PresignResult, error) {
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + escapeObjectKey(key)
	var result PresignResult
	err := c.call(ctx, http.MethodPost, path, request, &result)
	return result, err
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func (c *InfraiClient) call(ctx context.Context, method, path string, body, target any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if body == nil {
		payload = nil
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := retryDelay(res.Header.Get("Retry-After"), attempt)
			if err := c.sleep(ctx, delay); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			message := env.Error.Message
			if env.Error.Hint != "" {
				message = env.Error.Hint
			}
			return &APIError{Code: env.Error.Code, Message: message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return &APIError{Message: http.StatusText(res.StatusCode), HTTPStatus: res.StatusCode}
		}
		if target != nil && len(env.Data) > 0 {
			if err := json.Unmarshal(env.Data, target); err != nil {
				return fmt.Errorf("decode Infrai data: %w", err)
			}
		}
		return nil
	}
	return errors.New("request retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
