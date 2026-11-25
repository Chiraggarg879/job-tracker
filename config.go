package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Company describes one careers source to poll.
//
// Field usage per source:
//
//	greenhouse / lever / ashby : Slug (board name)
//	workday                    : Slug (tenant), Host (e.g. "wd5"), Site, Location (e.g. "India",
//	                             applied as the tenant's location facet), Query, Pages
//	amazon                     : Query, Location (ISO-3 country code, e.g. "IND"), Pages (of 100)
//	oracle                     : Host (pod, e.g. "eeho.fa.us2"), Slug (site number), Query, Location, Pages (of 25)
//	microsoft / google         : Query, Location (e.g. "India"), Pages
//	smartrecruiters            : Slug (company id), Query, Location (ISO-2 country code, e.g. "in"), Pages
//	atlassian                  : nothing extra
//
// Queries, when set, runs the search once per entry (in place of Query) and
// merges the results; search-based sources rank by relevance or cap results,
// so one query alone misses titles like "SDE-1".
type Company struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`
	Slug     string   `json:"slug,omitempty"`
	Host     string   `json:"host,omitempty"`
	Site     string   `json:"site,omitempty"`
	Query    string   `json:"query,omitempty"`
	Queries  []string `json:"queries,omitempty"`
	Location string   `json:"location,omitempty"`
	Pages    int      `json:"pages,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type FilterConfig struct {
	TitleInclude []string `json:"title_include"`
	TitleExclude []string `json:"title_exclude"`
	// Phrases that contain an excluded word but are fine, e.g.
	// "member of technical staff" (excluded word: "staff").
	TitleExcludeExceptions []string `json:"title_exclude_exceptions"`
	Locations              []string `json:"locations"`
	// Skip jobs whose description asks for more years than this.
	// Jobs that don't state a number are kept. 0 disables the check.
	MaxYearsExperience int `json:"max_years_experience"`
}

type Config struct {
	PollIntervalMinutes int            `json:"poll_interval_minutes"`
	StateFile           string         `json:"state_file"`
	Concurrency         int            `json:"concurrency"`
	Telegram            TelegramConfig `json:"telegram"`
	Filters             FilterConfig   `json:"filters"`
	Companies           []Company      `json:"companies"`
}

func (c *Config) PollInterval() time.Duration {
	return time.Duration(c.PollIntervalMinutes) * time.Minute
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.PollIntervalMinutes <= 0 {
		cfg.PollIntervalMinutes = 10
	}
	if cfg.StateFile == "" {
		cfg.StateFile = "seen_jobs.json"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}

	// Env vars win over the file so the token never has to be committed.
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := os.Getenv("TELEGRAM_CHAT_ID"); v != "" {
		cfg.Telegram.ChatID = v
	}

	for i, co := range cfg.Companies {
		if co.Name == "" || co.Source == "" {
			return nil, fmt.Errorf("company #%d: name and source are required", i+1)
		}
		if _, ok := fetchers[co.Source]; !ok {
			return nil, fmt.Errorf("company %q: unknown source %q", co.Name, co.Source)
		}
	}
	return &cfg, nil
}
