package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"superbot/internal/secrets"
	"superbot/internal/store"
)

// Factory turns a stored provider row into a concrete Provider. API keys are
// decrypted here and never leave the worker process.
type Factory struct {
	box *secrets.Box
}

func NewFactory(box *secrets.Box) *Factory {
	return &Factory{box: box}
}

func (f *Factory) Build(p store.Provider, timeout time.Duration) (Provider, error) {
	switch p.Kind {
	case "mock":
		return NewMock(p.Name), nil
	case "openai_compat":
		apiKey, err := f.decryptKey(p.ApiKeyEnc)
		if err != nil {
			return nil, err
		}
		if p.BaseUrl == "" {
			return nil, fmt.Errorf("provider %q has no base_url", p.Name)
		}
		return NewOpenAICompat(p.Name, p.BaseUrl, apiKey, timeout, WithEchoReasoning(echoesReasoning(p))), nil
	case "anthropic":
		apiKey, err := f.decryptKey(p.ApiKeyEnc)
		if err != nil {
			return nil, err
		}
		return NewAnthropic(p.Name, p.BaseUrl, apiKey, timeout), nil
	default:
		return nil, fmt.Errorf("unsupported provider kind %q", p.Kind)
	}
}

// echoesReasoning reports whether assistant tool-call turns must carry their
// reasoning back to the provider. DeepSeek's thinking mode rejects a tool
// loop without it; other OpenAI-compatible servers may reject the unknown
// field, so it is opt-in per provider ("echo_reasoning" in the config) and
// on by default only for DeepSeek.
func echoesReasoning(p store.Provider) bool {
	var cfg struct {
		Preset        string `json:"preset"`
		EchoReasoning *bool  `json:"echo_reasoning"`
	}
	_ = json.Unmarshal(p.Config, &cfg)
	if cfg.EchoReasoning != nil {
		return *cfg.EchoReasoning
	}
	return cfg.Preset == "deepseek" || strings.Contains(strings.ToLower(p.BaseUrl), "deepseek")
}

func (f *Factory) decryptKey(enc []byte) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	key, err := f.box.DecryptString(enc)
	if err != nil {
		return "", fmt.Errorf("decrypt provider api key: %w", err)
	}
	return key, nil
}
