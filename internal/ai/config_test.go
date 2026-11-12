package ai

import (
	"testing"
	"time"
)

func TestModelIdleTimeoutEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        time.Duration
	}{
		{"default", "", 120 * time.Second},
		{"explicit_old_value", "30s", 30 * time.Second},
		{"longer_wait", "5m", 5 * time.Minute},
		{"invalid_falls_back", "invalid", 120 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATASCOPE_AI_TIMEOUT", tc.value)
			client := FromEnv().(*ChatClient)
			if client.timeout != tc.want {
				t.Fatalf("idle timeout = %s, want %s", client.timeout, tc.want)
			}
		})
	}
}
