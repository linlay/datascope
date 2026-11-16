package permissions

import (
	"context"
	"datascope-server/tests/testutil"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"
)

func TestVisibleCustomerIDsMatchesFixtures(t *testing.T) {
	path := testutil.Demo(t)
	raw, err := os.ReadFile("../../../../tests/fixtures/securities/permission_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID       string   `json:"id"`
		UserID   int64    `json:"user_id"`
		Expected []string `json:"expected_ids"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, err := VisibleCustomerIDs(context.Background(), path, c.UserID, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		if !slices.Equal(got, c.Expected) {
			t.Errorf("%s: got %v, want %v", c.ID, got, c.Expected)
		}
	}
}
