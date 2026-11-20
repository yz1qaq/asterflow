package agent

import "time"

// EnvBinding is one agent environment pointer.
type EnvBinding struct {
	AgentID   string    `json:"agent_id"`
	Env       string    `json:"env"`
	VersionID string    `json:"version_id"`
	UpdatedAt time.Time `json:"updated_at"`
}
