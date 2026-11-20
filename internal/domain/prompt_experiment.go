package domain

import "time"

type ExperimentVariant struct {
	VersionID string `json:"version_id"`
	Traffic   int    `json:"traffic"`
}

type PromptExperiment struct {
	ID                 string              `json:"id"`
	PromptID           string              `json:"prompt_id"`
	Env                string              `json:"env"`
	BaselineVersionID  string              `json:"baseline_version_id,omitempty"`
	CandidateVersionID string              `json:"candidate_version_id,omitempty"`
	TrafficPercent     int                 `json:"traffic_percent"`
	Variants           []ExperimentVariant `json:"variants"`
	Status             string              `json:"status"`
	StartedAt          time.Time           `json:"started_at"`
	CompletedAt        *time.Time          `json:"completed_at,omitempty"`
	CreatedBy          string              `json:"created_by"`
	CreatedAt          time.Time           `json:"created_at"`
}

type PromptRolloutEvent struct {
	ID           string    `json:"id"`
	ExperimentID string    `json:"experiment_id"`
	Action       string    `json:"action"`
	FromTraffic  int       `json:"from_traffic"`
	ToTraffic    int       `json:"to_traffic"`
	Actor        string    `json:"actor"`
	Detail       string    `json:"detail"`
	CreatedAt    time.Time `json:"created_at"`
}

type PromptEnvEvent struct {
	ID            string    `json:"id"`
	PromptID      string    `json:"prompt_id"`
	Env           string    `json:"env"`
	Action        string    `json:"action"`
	FromVersionID string    `json:"from_version_id,omitempty"`
	ToVersionID   string    `json:"to_version_id"`
	Actor         string    `json:"actor"`
	Detail        string    `json:"detail"`
	CreatedAt     time.Time `json:"created_at"`
}
