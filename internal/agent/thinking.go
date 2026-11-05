package agent

import (
	"slices"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/discover"
)

// applyReasoning writes how hard this model should think into the request
// options of an OpenAI-compatible provider: llama.cpp's chat-template
// switch, or LM Studio's reasoning_effort.
//
// The spelling depends on the model and on the server. On llama.cpp a Qwen
// takes {"chat_template_kwargs": {"enable_thinking": bool}}; a gpt-oss takes
// {"chat_template_kwargs": {"reasoning_effort": "low"|"medium"|"high"}} and
// ignores enable_thinking outright, which is why the old global switch did
// nothing on it. LM Studio ignores chat_template_kwargs altogether and takes
// the request's own reasoning_effort (see lmstudioReasoning). A model with
// no reasoning axis, and a provider that is not OpenAI-compatible, are left
// untouched.
func applyReasoning(opts fantasy.ProviderOptions, model catwalk.Model, provider string, providerCfg config.ProviderConfig, cfg *config.Config) {
	level := cfg.ReasoningLevelOrDefault(provider, model)
	if level == "" {
		return
	}
	kind, _ := config.ReasoningAxis(model)
	setReasoning(opts, providerCfg, kind, level)
}

// quietReasoning asks for as little reasoning as the model allows, whatever
// the user chose. It is for requests whose answer is a few words, such as a
// session title, where a thinking model would otherwise spend its whole
// small budget reasoning and be cut off before answering.
//
// For a Qwen that is thinking off. For a gpt-oss there is no off — harmony
// always emits an analysis channel — so it is the lowest effort instead.
//
// A model whose id names no axis still gets enable_thinking false. That is
// the blanket switch this started as, and it is what protects a thinking
// model we failed to recognise: a server that does not know the key ignores
// it, while an unrecognised Qwen would otherwise reason through its whole
// title budget and leave every session "Untitled Session" again.
func quietReasoning(opts fantasy.ProviderOptions, model catwalk.Model, providerCfg config.ProviderConfig) {
	kind, levels := config.ReasoningAxis(model)
	if kind == config.ReasoningNone || len(levels) == 0 {
		setReasoning(opts, providerCfg, config.ReasoningThink, config.ReasoningNoThink)
		return
	}
	setReasoning(opts, providerCfg, kind, levels[0])
}

// setReasoning writes one level into opts in the spelling its kind and its
// server call for; other provider kinds are left untouched.
func setReasoning(opts fantasy.ProviderOptions, providerCfg config.ProviderConfig, kind config.ReasoningKind, level string) {
	if kind == config.ReasoningNone || level == "" {
		return
	}
	switch {
	case string(providerCfg.Type) == discover.TypeLMStudio:
		lmstudioReasoning(opts, kind, level)
	case string(providerCfg.Type) == openaicompat.Name, discover.IsKnownCustomProvider(string(providerCfg.Type)):
		kwargs := chatTemplateKwargs(opts)
		switch kind {
		case config.ReasoningThink:
			kwargs["enable_thinking"] = level == config.ReasoningThinkOn
		case config.ReasoningEffort:
			kwargs["reasoning_effort"] = level
		}
	}
}

// lmstudioEfforts are the values LM Studio's OpenAI-compatible endpoint
// takes for reasoning_effort; it lists them itself when refusing another.
var lmstudioEfforts = []openai.ReasoningEffort{
	openai.ReasoningEffortNone,
	openai.ReasoningEffortMinimal,
	openai.ReasoningEffortLow,
	openai.ReasoningEffortMedium,
	openai.ReasoningEffortHigh,
	openai.ReasoningEffortXHigh,
}

// lmstudioReasoning writes level for LM Studio, which ignores
// chat_template_kwargs and takes the request's reasoning_effort instead:
// nothink is "none", which turns a Qwen's thinking off, and an effort level
// goes as it is. think sends nothing, so the reasoning setting chosen for
// the model in LM Studio stays in force; nor is a level LM Studio does not
// take sent, as it would refuse the whole request.
func lmstudioReasoning(opts fantasy.ProviderOptions, kind config.ReasoningKind, level string) {
	var effort openai.ReasoningEffort
	switch kind {
	case config.ReasoningThink:
		if level != config.ReasoningNoThink {
			return
		}
		effort = openai.ReasoningEffortNone
	case config.ReasoningEffort:
		effort = openai.ReasoningEffort(level)
		if !slices.Contains(lmstudioEfforts, effort) {
			return
		}
	default:
		return
	}
	compatOptions(opts).ReasoningEffort = &effort
}

// compatOptions returns the request's OpenAI-compatible options, making
// them as needed.
func compatOptions(opts fantasy.ProviderOptions) *openaicompat.ProviderOptions {
	po, _ := opts[openaicompat.Name].(*openaicompat.ProviderOptions)
	if po == nil {
		po = &openaicompat.ProviderOptions{}
		opts[openaicompat.Name] = po
	}
	return po
}

// chatTemplateKwargs returns the request's chat_template_kwargs map, making
// it and everything above it as needed, so a caller can write one key
// without disturbing the others already there.
func chatTemplateKwargs(opts fantasy.ProviderOptions) map[string]any {
	po := compatOptions(opts)
	if po.ExtraBody == nil {
		po.ExtraBody = map[string]any{}
	}
	kwargs, _ := po.ExtraBody["chat_template_kwargs"].(map[string]any)
	if kwargs == nil {
		kwargs = map[string]any{}
	}
	po.ExtraBody["chat_template_kwargs"] = kwargs
	return kwargs
}
