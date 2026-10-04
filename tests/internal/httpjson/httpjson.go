// Package httpjson выполняет JSON-запросы к уже запущенному Gateway.
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Response — статус и разобранное тело. Пустое тело даёт пустую карту.
type Response struct {
	Status int
	Body   map[string]any
	Raw    string
}

// Do отправляет запрос и читает ответ целиком.
func Do(ctx context.Context, method, rawURL string, header map[string]string, body any) (Response, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("encode body: %w", err)
		}

		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, payload)
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for key, value := range header {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	decoded := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return Response{Status: resp.StatusCode, Raw: string(raw)}, fmt.Errorf("decode response: %w", err)
		}
	}

	return Response{Status: resp.StatusCode, Body: decoded, Raw: string(raw)}, nil
}
