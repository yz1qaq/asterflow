package prompt

import (
	"time"

	"github.com/yz1qaq/asterflow/internal/domain"
)

type Prompt struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PromptVersion struct {
	Archived              bool                    `json:"-"`
	ID                    string                  `json:"id"`
	PromptID              string                  `json:"prompt_id"`
	Version               int                     `json:"version"`
	Template              string                  `json:"template"`
	VariablesSchema       string                  `json:"variables_schema"`
	ModelProfileVersionID string                  `json:"model_profile_version_id,omitempty"`
	GenerationConfig      domain.GenerationConfig `json:"generation_config"`
	Hash                  string                  `json:"hash"`
	TokenEstimate         int                     `json:"token_estimate"`
	CreatedBy             string                  `json:"created_by"`
	CreatedAt             time.Time               `json:"created_at"`
}

type EnvBinding struct {
	PromptID  string    `json:"prompt_id"`
	Env       string    `json:"env"`
	VersionID string    `json:"version_id"`
	UpdatedAt time.Time `json:"updated_at"`
}
