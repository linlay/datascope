package platform_test

import (
	"context"
	"encoding/json"

	"datascope-server/internal/ai"
)

// A second consumer with its own prompt, input and output, used only in tests.
func extractTerms(ctx context.Context, model ai.Client, text string) ([]string, error) {
	res, err := model.Complete(ctx, ai.Request{Messages: []ai.Message{{Role: "system", Content: "Extract vocabulary as JSON terms."}, {Role: "user", Content: text}}, JSONObject: true})
	if err != nil {
		return nil, err
	}
	var out struct {
		Terms []string `json:"terms"`
	}
	err = json.Unmarshal([]byte(res.Text), &out)
	return out.Terms, err
}
