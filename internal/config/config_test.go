package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"steadily/internal/config"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		expectErr bool
	}{
		{
			name: "valid l7 config",
			yaml: `
listen_address: ":8080"
mode: "l7"
algorithm: "round_robin"
shutdown_timeout: "5s"
health_check:
  path: "/health"
  interval: "1s"
  timeout: "500ms"
  healthy_threshold: 2
  unhealthy_threshold: 2
backends:
  - name: "b1"
    address: "127.0.0.1:8081"
  - name: "b2"
    address: "127.0.0.1:8082"
`,
			expectErr: false,
		},
		{
			name: "invalid mode",
			yaml: `
listen_address: ":8080"
mode: "invalid_mode"
algorithm: "round_robin"
backends:
  - name: "b1"
    address: "127.0.0.1:8081"
`,
			expectErr: true,
		},
		{
			name: "invalid algorithm",
			yaml: `
listen_address: ":8080"
mode: "l7"
algorithm: "invalid_algo"
backends:
  - name: "b1"
    address: "127.0.0.1:8081"
`,
			expectErr: true,
		},
		{
			name: "timeout greater than interval",
			yaml: `
listen_address: ":8080"
mode: "l7"
algorithm: "round_robin"
health_check:
  interval: "500ms"
  timeout: "1s"
  healthy_threshold: 2
  unhealthy_threshold: 2
backends:
  - name: "b1"
    address: "127.0.0.1:8081"
`,
			expectErr: true,
		},
		{
			name: "duplicate backend names",
			yaml: `
listen_address: ":8080"
mode: "l7"
algorithm: "round_robin"
backends:
  - name: "b1"
    address: "127.0.0.1:8081"
  - name: "b1"
    address: "127.0.0.1:8082"
`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}

			cfg, err := config.Load(path)
			if tt.expectErr && err == nil {
				t.Fatalf("expected error for test '%s', got nil", tt.name)
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("unexpected error for test '%s': %v", tt.name, err)
			}
			if !tt.expectErr && cfg != nil {
				if cfg.ShutdownTimeout != 5*time.Second {
					t.Errorf("expected 5s shutdown timeout, got %v", cfg.ShutdownTimeout)
				}
			}
		})
	}
}
