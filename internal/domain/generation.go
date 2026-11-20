package domain

type GenerationConfig struct {
	Temperature     *float32 `json:"temperature,omitempty"`
	TopP            *float32 `json:"top_p,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
	Stop            []string `json:"stop,omitempty"`
	Seed            *int     `json:"seed,omitempty"`
}
