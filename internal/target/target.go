package target

import (
	"fmt"
	"strings"
)

type Type string

const (
	TypeOrg  Type = "org"
	TypeRepo Type = "repo"
)

type Target struct {
	Type          Type   `json:"type"`
	Org           string `json:"org,omitempty"`
	Owner         string `json:"owner,omitempty"`
	Repo          string `json:"repo,omitempty"`
	RunnerGroupID int64  `json:"runner_group_id,omitempty"`
}

type ConfigYAML struct {
	Org           string `yaml:"org,omitempty"`
	Repo          string `yaml:"repo,omitempty"`
	RunnerGroupID int64  `yaml:"runner_group_id,omitempty"`
}

func FromConfig(cfg ConfigYAML) (Target, error) {
	hasOrg := strings.TrimSpace(cfg.Org) != ""
	hasRepo := strings.TrimSpace(cfg.Repo) != ""

	switch {
	case hasOrg && hasRepo:
		return Target{}, fmt.Errorf("target.org and target.repo are mutually exclusive")
	case hasOrg:
		if cfg.RunnerGroupID <= 0 {
			return Target{}, fmt.Errorf("target.runner_group_id is required for org target")
		}
		return Target{
			Type:          TypeOrg,
			Org:           strings.ToLower(strings.TrimSpace(cfg.Org)),
			RunnerGroupID: cfg.RunnerGroupID,
		}, nil
	case hasRepo:
		owner, repo, err := parseRepo(cfg.Repo)
		if err != nil {
			return Target{}, err
		}
		return Target{
			Type:  TypeRepo,
			Owner: owner,
			Repo:  repo,
		}, nil
	default:
		return Target{}, fmt.Errorf("target.org or target.repo is required")
	}
}

func parseRepo(value string) (owner, repo string, err error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("target.repo must be in owner/repo format")
	}
	return parts[0], parts[1], nil
}

func (t Target) String() string {
	switch t.Type {
	case TypeOrg:
		return fmt.Sprintf("org:%s", t.Org)
	case TypeRepo:
		return fmt.Sprintf("repo:%s/%s", t.Owner, t.Repo)
	default:
		return "unknown"
	}
}

func (t Target) DisplayString() string {
	if t.Type == TypeOrg && t.RunnerGroupID > 0 {
		return fmt.Sprintf("%s (group %d)", t.String(), t.RunnerGroupID)
	}
	return t.String()
}

func (t Target) Validate() error {
	switch t.Type {
	case TypeOrg:
		if t.Org == "" {
			return fmt.Errorf("org target requires org")
		}
		if t.RunnerGroupID <= 0 {
			return fmt.Errorf("org target requires runner_group_id")
		}
	case TypeRepo:
		if t.Owner == "" || t.Repo == "" {
			return fmt.Errorf("repo target requires owner and repo")
		}
	default:
		return fmt.Errorf("unknown target type %q", t.Type)
	}
	return nil
}

func RequireOrg(tgt Target) error {
	if tgt.Type != TypeOrg {
		return fmt.Errorf("broker supports organization targets only")
	}
	return tgt.Validate()
}
