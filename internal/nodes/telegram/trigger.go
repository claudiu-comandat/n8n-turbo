package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type Trigger struct {
	client  *http.Client
	baseURL string
}

func NewTrigger(baseURL string) *Trigger {
	return &Trigger{client: &http.Client{Timeout: 30 * time.Second}, baseURL: baseURL}
}

func (t *Trigger) SetupWebhook(ctx context.Context, credential *Credential, webhookURL string, secretToken string, allowed []string) error {
	if credential == nil {
		return fmt.Errorf("credential is required")
	}
	credential.baseURL = t.baseURL
	body := map[string]any{"url": webhookURL}
	if secretToken != "" {
		body["secret_token"] = secretToken
	}
	if len(allowed) > 0 {
		body["allowed_updates"] = allowed
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, credential.BaseURL()+"/setWebhook", bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := t.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, err = decodeResponse(response)
	return err
}

func (t *Trigger) HandleWebhook(body []byte, receivedSecret string, expectedSecret string) (dataplane.Item, error) {
	if expectedSecret != "" && receivedSecret != expectedSecret {
		return dataplane.Item{}, fmt.Errorf("invalid Telegram webhook secret token")
	}
	var update map[string]any
	if err := json.Unmarshal(body, &update); err != nil {
		return dataplane.Item{}, err
	}
	return dataplane.Item{JSON: update}, nil
}

func (t *Trigger) Poll(ctx context.Context, credential *Credential, lastUpdateID int64) ([]dataplane.Item, int64, error) {
	if credential == nil {
		return nil, lastUpdateID, fmt.Errorf("credential is required")
	}
	credential.baseURL = t.baseURL
	body := map[string]any{"offset": lastUpdateID + 1, "limit": 100, "timeout": 30, "allowed_updates": []string{"message", "callback_query"}}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, lastUpdateID, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, credential.BaseURL()+"/getUpdates", bytes.NewReader(data))
	if err != nil {
		return nil, lastUpdateID, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := t.client.Do(request)
	if err != nil {
		return nil, lastUpdateID, err
	}
	defer response.Body.Close()
	result, err := decodeResponse(response)
	if err != nil {
		return nil, lastUpdateID, err
	}
	updates, _ := result["result"].([]any)
	items := make([]dataplane.Item, 0, len(updates))
	maxID := lastUpdateID
	for _, update := range updates {
		object, ok := update.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := object["update_id"].(float64); ok && int64(value) > maxID {
			maxID = int64(value)
		}
		items = append(items, dataplane.Item{JSON: object})
	}
	return items, maxID, nil
}

func ReadWebhookBody(reader io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(reader, 2*1024*1024))
}
