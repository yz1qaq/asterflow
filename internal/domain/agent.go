package domain

import "time"

type Agent struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Template    string    `json:"template"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AgentVersion struct {
	ID           string    `json:"id"`
	AgentID      string    `json:"agent_id"`
	Version      int       `json:"version"`
	SnapshotJSON string    `json:"snapshot_json"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}
