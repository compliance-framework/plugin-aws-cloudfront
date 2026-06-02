package internal

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PluginConfig struct {
	PolicyLabels map[string]string
}

func ParseConfig(raw map[string]string) (*PluginConfig, error) {
	config := &PluginConfig{}
	if v := strings.TrimSpace(raw["policy_labels"]); v != "" {
		if err := json.Unmarshal([]byte(v), &config.PolicyLabels); err != nil {
			return nil, fmt.Errorf("could not parse policy_labels: %w", err)
		}
	}
	return config, nil
}
