package reporter

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
)

const (
	InstallerConfigPath = "/aegispxe/reporter.json"
	SystemConfigPath    = "/etc/aegispxe/reporter.json"
)

type Config struct {
	APIBase        string `json:"api_base"`
	InstallationID string `json:"installation_id"`
	MachineID      string `json:"machine_id"`
	AdminUsername  string `json:"admin_username"`
}

func LoadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	parsed, err := url.Parse(strings.TrimSpace(c.APIBase))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("reporter API base is invalid")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("reporter API base must not contain a path")
	}
	if strings.TrimSpace(c.InstallationID) == "" || strings.TrimSpace(c.MachineID) == "" {
		return errors.New("reporter installation and machine IDs are required")
	}
	if username := strings.TrimSpace(c.AdminUsername); username == "" || len(username) > 32 {
		return errors.New("reporter admin username is invalid")
	}
	return nil
}

func (c Config) endpoint(suffix string) string {
	return strings.TrimRight(c.APIBase, "/") + "/api/v1/installations/" + url.PathEscape(c.InstallationID) + suffix
}
