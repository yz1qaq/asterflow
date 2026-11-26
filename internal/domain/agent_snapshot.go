package domain

type AgentSnapshot struct {
	ID                    string           `json:"id"`
	SystemPrompt          string           `json:"system_prompt,omitempty"`
	SystemPromptID        string           `json:"system_prompt_id,omitempty"`
	SystemPromptVersionID string           `json:"system_prompt_version_id,omitempty"`
	UserPromptID          string           `json:"user_prompt_id,omitempty"`
	UserPromptVersionID   string           `json:"user_prompt_version_id,omitempty"`
	PromptEnv             string           `json:"prompt_env,omitempty"`
	ModelProfileVersionID string           `json:"model_profile_version_id,omitempty"`
	GenerationConfig      GenerationConfig `json:"generation_config"`
	ToolIDs               []string         `json:"tool_ids,omitempty"`
	ToolVersionIDs        []string         `json:"tool_version_ids,omitempty"`
}

type PromptSelection struct {
	VersionID         string
	ExperimentID      string
	ExperimentVariant string
}

type ConversationRuntimeConfig struct {
	Environment           string           `json:"environment,omitempty"`
	SystemPrompt          string           `json:"system_prompt,omitempty"`
	SystemPromptVersionID string           `json:"system_prompt_version_id,omitempty"`
	UserPromptVersionID   string           `json:"user_prompt_version_id,omitempty"`
	ModelProfileVersionID string           `json:"model_profile_version_id,omitempty"`
	GenerationConfig      GenerationConfig `json:"generation_config"`
	UserPromptExperiment  string           `json:"user_prompt_experiment,omitempty"`
	UserPromptVariant     string           `json:"user_prompt_variant,omitempty"`
}

type UserPromptSnapshot struct {
	ExperimentID      string         `json:"user_prompt_experiment"`
	ExperimentVariant string         `json:"user_prompt_variant"`
	VersionID         string         `json:"user_prompt_version_id"`
	Variables         map[string]any `json:"user_prompt_variables"`
	Rendered          string         `json:"rendered_user_prompt"`
}
