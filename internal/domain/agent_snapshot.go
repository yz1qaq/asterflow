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
}

type ConversationRuntimeConfig struct {
	Environment           string           `json:"environment,omitempty"`
	SystemPrompt          string           `json:"system_prompt,omitempty"`
	SystemPromptVersionID string           `json:"system_prompt_version_id,omitempty"`
	UserPromptVersionID   string           `json:"user_prompt_version_id,omitempty"`
	ModelProfileVersionID string           `json:"model_profile_version_id,omitempty"`
	GenerationConfig      GenerationConfig `json:"generation_config"`
}
