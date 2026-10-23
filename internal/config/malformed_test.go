package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Prabodh-dev/steadily/internal/config"
)

const validBase = `
listen_address: ":8080"
mode: "l7"
algorithm: "round_robin"
health_check:
  interval: "1s"
  timeout: "500ms"
  healthy_threshold: 1
  unhealthy_threshold: 1
backends:
  - name: "b1"
    address: "127.0.0.1:8081"
`

var malformedConfigs = map[string]string{
	"empty document":          ``,
	"scalar document":         `just a string`,
	"list document":           `- a\n- b`,
	"null backend entry":      strings.Replace(validBase, `  - name: "b1"`, `  - null`, 1),
	"wrong type for backends": strings.Replace(validBase, "backends:", "backends: 7\nignored:", 1),
	"negative duration":       strings.Replace(validBase, `"1s"`, `"-1s"`, 1),
	"overflow duration":       strings.Replace(validBase, `"1s"`, `"999999999999h"`, 1),
	"garbage duration":        strings.Replace(validBase, `"1s"`, `"soon"`, 1),
	"address without port":    strings.Replace(validBase, `127.0.0.1:8081`, `127.0.0.1`, 1),
	"unknown group backend":   validBase + "groups:\n  - name: g\n    backends: [missing]\n",
	"route unknown group":     validBase + "routes:\n  - path_prefix: /\n    group: missing\n",
	"timeout above interval":  strings.Replace(validBase, `"500ms"`, `"2s"`, 1),
	"zero thresholds":         strings.Replace(validBase, "healthy_threshold: 1", "healthy_threshold: 0", 1),
	"alias expansion": `
a: &a [x, x, x, x, x, x, x, x, x, x]
b: &b [*a, *a, *a, *a, *a, *a, *a, *a, *a, *a]
c: &c [*b, *b, *b, *b, *b, *b, *b, *b, *b, *b]
d: &d [*c, *c, *c, *c, *c, *c, *c, *c, *c, *c]
e: &e [*d, *d, *d, *d, *d, *d, *d, *d, *d, *d]
f: &f [*e, *e, *e, *e, *e, *e, *e, *e, *e, *e]
g: &g [*f, *f, *f, *f, *f, *f, *f, *f, *f, *f]
h: &h [*g, *g, *g, *g, *g, *g, *g, *g, *g, *g]
i: &i [*h, *h, *h, *h, *h, *h, *h, *h, *h, *h]
backends: *i
`,
}

func writeConfig(t testing.TB, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "steadily.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadRejectsMalformedInput(t *testing.T) {
	for name, content := range malformedConfigs {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(writeConfig(t, content)); err == nil {
				t.Fatalf("expected an error for malformed config")
			}
		})
	}
}

func FuzzLoad(f *testing.F) {
	f.Add(validBase)
	f.Add(validBase + "groups:\n  - null\n")
	f.Add(validBase + "routes:\n  - null\n")
	for _, content := range malformedConfigs {
		f.Add(content)
	}
	f.Fuzz(func(t *testing.T, content string) {
		_, _ = config.Load(writeConfig(t, content))
	})
}
