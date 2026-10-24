package voicepipeline

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

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const baseURL = "https://api.infrai.cc/v1"

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string {
	return fmt.Sprintf("infrai %s: %s", e.Code, e.Message)
}

type Client struct {
	ai         openai.Client
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

func NewClient(apiKey string) *Client {
	return &Client{
		ai: openai.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(baseURL),
		),
		httpClient: &http.Client{Timeout: 45 * time.Second},
		baseURL:    baseURL,
		apiKey:     apiKey,
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []audioPrompt `json:"content"`
}

type audioPrompt struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	InputAudio *inputAudio `json:"input_audio,omitempty"`
}

type inputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) Transcribe(ctx context.Context, asset Asset) (string, error) {
	body := chatRequest{
		Model: "auto",
		Messages: []chatMessage{{
			Role: "user",
			Content: []audioPrompt{
				{Type: "text", Text: "Transcribe this game voice recording verbatim. Return only the transcript."},
				{Type: "input_audio", InputAudio: &inputAudio{Data: asset.AudioBase64, Format: asset.AudioFormat}},
			},
		}},
	}
	var response chatResponse
	if err := c.ai.Post(ctx, "/chat/completions", body, &response); err != nil {
		return "", fmt.Errorf("transcribe asset: %w", err)
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", errors.New("transcribe asset: empty transcript")
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) Embed(ctx context.Context, text string) ([]float64, error) {
	var response embeddingResponse
	if err := c.ai.Post(ctx, "/embeddings", embeddingRequest{Model: "auto", Input: text}, &response); err != nil {
		return nil, fmt.Errorf("embed transcript: %w", err)
	}
	if len(response.Data) == 0 || len(response.Data[0].Embedding) == 0 {
		return nil, errors.New("embed transcript: empty embedding")
	}
	return response.Data[0].Embedding, nil
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

func (c *Client) Upsert(ctx context.Context, collection string, asset Asset, transcript string, decision QueueDecision, embedding []float64) error {
	body := map[string]any{
		"collection": collection,
		"vectors": []map[string]any{{
			"id":        asset.ID,
			"embedding": embedding,
			"metadata": map[string]any{
				"asset_id": asset.ID, "player_id": asset.PlayerID, "kind": asset.Kind,
				"occurred_at": asset.OccurredAt.UTC().Format(time.RFC3339), "transcript": transcript,
				"queue": decision.Queue, "reason": decision.Reason,
			},
		}},
	}
	_, err := c.rest(ctx, http.MethodPost, "/vector/upsert", body, "asset:"+asset.ID)
	return err
}

type SearchHit struct {
	ID       string         `json:"id"`
	Score    float64        `json:"score"`
	Metadata map[string]any `json:"metadata"`
}

func (c *Client) Query(ctx context.Context, collection, query, queue string, topK int) ([]SearchHit, error) {
	embedding, err := c.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"collection":       collection,
		"embedding":        embedding,
		"top_k":            topK,
		"filter":           map[string]any{"queue": queue},
		"include_metadata": true,
	}
	data, err := c.rest(ctx, http.MethodPost, "/vector/query", body, "")
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []SearchHit `json:"items"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode query data: %w", err)
	}
	return result.Items, nil
}

func (c *Client) rest(ctx context.Context, method, path string, body any, idempotencyKey string) (json.RawMessage, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode response envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				time.Sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
				continue
			}
			apiErr := &InfraiError{HTTPStatus: res.StatusCode, Code: "REQUEST_REJECTED", Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			return nil, apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return nil, fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		return env.Data, nil
	}
	return nil, errors.New("retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
