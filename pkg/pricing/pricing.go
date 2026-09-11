package pricing

// Rate uses micro-USD per token. Numerically this equals USD per million
// tokens, but integral values below are represented in tenths of a micro-USD.
// Keeping tenths avoids rounding small requests (e.g. $1.40 / MTok).
type Rate struct {
	Model  string `json:"model"`
	Input  *int64 `json:"input_tenth_micro_usd"`
	Output *int64 `json:"output_tenth_micro_usd"`
	Source string `json:"source"`
	URL    string `json:"url"`
	Note   string `json:"note"`
}

const Version = "public-base-2026-09-11-v1"
const CollectedAt = "2026-09-11"

func n(v int64) *int64 { return &v }

// Prices are reference base API rates, NOT JoyCode invoices. Unknown
// deployments deliberately have no rate; no prefix-based fallback is used.
var Rates = []Rate{
	{"GPT-6 Astra", n(100), n(500), "Artificial Analysis", "https://artificialanalysis.ai/models/gpt-6-astra", "公开基础价参考；非 JoyCode 账单"},
	{"GPT-5.6 Sol", n(40), n(200), "Artificial Analysis", "https://artificialanalysis.ai/models/gpt-5-6-sol", "未计长上下文/服务层级等附加费"},
	{"Claude-Opus-5", n(50), n(250), "Claude 官方", "https://platform.claude.com/docs/en/about-claude/pricing", "第一方标准价；非 Bedrock 区域账单"},
	{"Claude-Opus-4.8", n(50), n(250), "Claude 官方", "https://platform.claude.com/docs/en/about-claude/pricing", "第一方标准价"},
	{"Claude-Opus-4.7", n(50), n(250), "Claude 官方", "https://platform.claude.com/docs/en/about-claude/pricing", "历史型号参考价"},
	{"Claude-Opus-4.6", n(50), n(250), "Claude 官方", "https://platform.claude.com/docs/en/about-claude/pricing", "历史型号参考价"},
	{"Claude-Sonnet-4.6", n(30), n(150), "Claude 官方", "https://platform.claude.com/docs/en/about-claude/pricing", "历史型号参考价"},
	{"GLM-5.3", n(14), n(44), "Artificial Analysis", "https://artificialanalysis.ai/models/glm-5-3", "公开基础价参考"},
	{"Kimi-K3", n(30), n(150), "Artificial Analysis", "https://artificialanalysis.ai/models/kimi-k3", "公开基础价参考"},
	{"MiniMax-M3", n(3), n(12), "Artificial Analysis", "https://artificialanalysis.ai/models/minimax-m3", "公开基础价参考"},
	{"GLM-5.2-jcloud", nil, nil, "", "", "部署价格未确认，不继承基础模型价格"},
	{"Kimi-K3-jcloud", nil, nil, "", "", "部署价格未确认，不继承基础模型价格"},
	{"DeepSeek-V4-Pro", nil, nil, "", "", "部署日期版本未确认，暂不计价"},
	{"Doubao-Seed-2.0-pro", nil, nil, "", "", "本次未取得可信匹配价格"},
	{"JoyAI-Code-1.5", nil, nil, "", "", "本次未取得可信匹配价格"},
	{"JoyCode-Base-V3", nil, nil, "", "", "本次未取得可信匹配价格"},
}

func Lookup(model string) Rate {
	aliases := map[string]string{
		"Claude-Opus-5-hq": "Claude-Opus-5", "Claude-Opus-4.8-hq": "Claude-Opus-4.8",
		"Claude-Opus-4.7-hq": "Claude-Opus-4.7", "Claude-Opus-4.6-hq": "Claude-Opus-4.6", "Claude-Sonnet-4.6-hq": "Claude-Sonnet-4.6",
	}
	if alias, ok := aliases[model]; ok {
		model = alias
	}
	for _, r := range Rates {
		if r.Model == model {
			return r
		}
	}
	return Rate{Model: model, Note: "无可核实单价"}
}
