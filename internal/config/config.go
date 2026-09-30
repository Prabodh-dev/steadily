package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type HealthCheckConfig struct {
	Path               string        `yaml:"path"`
	Interval           time.Duration `yaml:"-"`
	IntervalRaw        string        `yaml:"interval"`
	Timeout            time.Duration `yaml:"-"`
	TimeoutRaw         string        `yaml:"timeout"`
	HealthyThreshold   int           `yaml:"healthy_threshold"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold"`
}

type BackendConfig struct {
	Name    string `yaml:"name"`
	Address string `yaml:"address"`
	Weight  int    `yaml:"weight"`
}

type GroupConfig struct {
	Name     string   `yaml:"name"`
	Backends []string `yaml:"backends"`
}

type RouteConfig struct {
	PathPrefix string `yaml:"path_prefix"`
	Group      string `yaml:"group"`
}

type Config struct {
	ListenAddress   string            `yaml:"listen_address"`
	MetricsAddress  string            `yaml:"metrics_address"`
	Mode            string            `yaml:"mode"`
	Algorithm       string            `yaml:"algorithm"`
	ShutdownTimeout time.Duration     `yaml:"-"`
	ShutdownRaw     string            `yaml:"shutdown_timeout"`
	HealthCheck     HealthCheckConfig `yaml:"health_check"`
	Backends        []BackendConfig   `yaml:"backends"`
	Groups          []GroupConfig     `yaml:"groups"`
	Routes          []RouteConfig     `yaml:"routes"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv("STEADILY_CONFIG")
	}
	if path == "" {
		return nil, fmt.Errorf("config path not provided")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml config: %w", err)
	}

	if err := cfg.parseDurations(); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) parseDurations() error {
	if c.ShutdownRaw != "" {
		d, err := time.ParseDuration(c.ShutdownRaw)
		if err != nil {
			return fmt.Errorf("invalid shutdown_timeout '%s': %w", c.ShutdownRaw, err)
		}
		c.ShutdownTimeout = d
	} else {
		c.ShutdownTimeout = 10 * time.Second
	}

	if c.HealthCheck.IntervalRaw != "" {
		d, err := time.ParseDuration(c.HealthCheck.IntervalRaw)
		if err != nil {
			return fmt.Errorf("invalid health_check interval '%s': %w", c.HealthCheck.IntervalRaw, err)
		}
		c.HealthCheck.Interval = d
	}

	if c.HealthCheck.TimeoutRaw != "" {
		d, err := time.ParseDuration(c.HealthCheck.TimeoutRaw)
		if err != nil {
			return fmt.Errorf("invalid health_check timeout '%s': %w", c.HealthCheck.TimeoutRaw, err)
		}
		c.HealthCheck.Timeout = d
	}

	return nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.ListenAddress) == "" {
		return fmt.Errorf("listen_address cannot be empty")
	}

	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode != "l4" && c.Mode != "l7" {
		return fmt.Errorf("invalid mode '%s': must be 'l4' or 'l7'", c.Mode)
	}

	c.Algorithm = strings.ToLower(strings.TrimSpace(c.Algorithm))
	if c.Algorithm != "round_robin" && c.Algorithm != "least_connections" {
		return fmt.Errorf("invalid algorithm '%s': must be 'round_robin' or 'least_connections'", c.Algorithm)
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be greater than 0")
	}

	if c.HealthCheck.Interval <= 0 {
		return fmt.Errorf("health_check interval must be greater than 0")
	}
	if c.HealthCheck.Timeout <= 0 {
		return fmt.Errorf("health_check timeout must be greater than 0")
	}
	if c.HealthCheck.Timeout >= c.HealthCheck.Interval {
		return fmt.Errorf("health_check timeout must be strictly less than interval")
	}
	if c.HealthCheck.HealthyThreshold < 1 {
		return fmt.Errorf("health_check healthy_threshold must be at least 1")
	}
	if c.HealthCheck.UnhealthyThreshold < 1 {
		return fmt.Errorf("health_check unhealthy_threshold must be at least 1")
	}

	if c.Mode == "l7" {
		if c.HealthCheck.Path == "" {
			c.HealthCheck.Path = "/health"
		}
		if !strings.HasPrefix(c.HealthCheck.Path, "/") {
			return fmt.Errorf("health_check path must start with '/'")
		}
	}

	if len(c.Backends) == 0 {
		return fmt.Errorf("backends list cannot be empty")
	}

	backendNames := make(map[string]bool)
	for i, b := range c.Backends {
		if strings.TrimSpace(b.Name) == "" {
			return fmt.Errorf("backend at index %d has empty name", i)
		}
		if backendNames[b.Name] {
			return fmt.Errorf("duplicate backend name '%s'", b.Name)
		}
		backendNames[b.Name] = true

		if strings.TrimSpace(b.Address) == "" {
			return fmt.Errorf("backend '%s' has empty address", b.Name)
		}
		if _, _, err := net.SplitHostPort(b.Address); err != nil {
			return fmt.Errorf("backend '%s' address '%s' is invalid: %w", b.Name, b.Address, err)
		}
		if b.Weight < 1 {
			c.Backends[i].Weight = 1
		}
	}

	groupMap := make(map[string]bool)
	for i, g := range c.Groups {
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("group at index %d has empty name", i)
		}
		if groupMap[g.Name] {
			return fmt.Errorf("duplicate group name '%s'", g.Name)
		}
		groupMap[g.Name] = true

		if len(g.Backends) == 0 {
			return fmt.Errorf("group '%s' has no backends", g.Name)
		}
		for _, bName := range g.Backends {
			if !backendNames[bName] {
				return fmt.Errorf("group '%s' references unknown backend '%s'", g.Name, bName)
			}
		}
	}

	if len(c.Groups) == 0 {
		var allNames []string
		for _, b := range c.Backends {
			allNames = append(allNames, b.Name)
		}
		c.Groups = []GroupConfig{
			{
				Name:     "default",
				Backends: allNames,
			},
		}
		groupMap["default"] = true
	}

	if c.Mode == "l7" {
		if len(c.Routes) == 0 {
			c.Routes = []RouteConfig{
				{
					PathPrefix: "/",
					Group:      c.Groups[0].Name,
				},
			}
		} else {
			for i, r := range c.Routes {
				if !strings.HasPrefix(r.PathPrefix, "/") {
					return fmt.Errorf("route at index %d path_prefix must start with '/'", i)
				}
				if !groupMap[r.Group] {
					return fmt.Errorf("route '%s' references unknown group '%s'", r.PathPrefix, r.Group)
				}
			}
		}
	}

	return nil
}
