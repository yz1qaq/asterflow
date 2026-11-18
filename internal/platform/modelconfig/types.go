package modelconfig

import "time"

const DefaultDeploymentTimeoutMS = 120000

type ProviderAccount struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	BaseURL     string    `json:"base_url"`
	Status      string    `json:"status"`
	HasAPIKey   bool      `json:"has_api_key"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type providerAccountRecord struct {
	ProviderAccount
	APIKeyCiphertext []byte `json:"-"`
}

type ModelDeployment struct {
	ID                         string    `json:"id"`
	WorkspaceID                string    `json:"workspace_id"`
	ProviderAccountID          string    `json:"provider_account_id"`
	Name                       string    `json:"name"`
	ModelName                  string    `json:"model_name"`
	Region                     string    `json:"region"`
	TimeoutMS                  int       `json:"timeout_ms"`
	MaxRetries                 int       `json:"max_retries"`
	InputPricePerMillion       float64   `json:"input_price_per_million"`
	OutputPricePerMillion      float64   `json:"output_price_per_million"`
	CachedInputPricePerMillion float64   `json:"cached_input_price_per_million"`
	Status                     string    `json:"status"`
	CreatedBy                  string    `json:"created_by"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

// 模型方案
type ModelProfile struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ModelProfileVersion struct {
	ID                    string    `json:"id"`
	ProfileID             string    `json:"profile_id"`
	Version               int       `json:"version"`
	PrimaryDeploymentID   string    `json:"primary_deployment_id"`
	FallbackDeploymentIDs []string  `json:"fallback_deployment_ids"`
	ClassificationMax     string    `json:"classification_max"`
	CreatedBy             string    `json:"created_by"`
	CreatedAt             time.Time `json:"created_at"`
}

type ProjectBinding struct {
	WorkspaceID           string    `json:"workspace_id"`
	Env                   string    `json:"env"`
	ModelProfileVersionID string    `json:"model_profile_version_id"`
	MonthlyBudget         float64   `json:"monthly_budget"`
	RPMLimit              int       `json:"rpm_limit"` //每分钟请求数
	TPMLimit              int       `json:"tpm_limit"` //每分钟token数
	UpdatedAt             time.Time `json:"updated_at"`
}
