package service

import (
	"context"
	"testing"
)

func TestInitializationStillRejectsMissingPolicyColumn(t *testing.T) {
	svc := newService(t)
	cfg := svc.Config()
	policy := cfg.Tables[0]
	policy.Columns = []string{"__missing_policy_column__"}
	if _, err := New(context.Background(), svc.Store, cfg, svc.AsOf); err == nil {
		t.Fatal("initialization accepted a nonexistent policy column")
	}
}
