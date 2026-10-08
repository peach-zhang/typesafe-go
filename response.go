package typesafe

// 答案类型常量,对应 Answer.Type 与问题构造函数。
const (
	// TypeNoul 对应 Noul 问题。
	TypeNoul = "noul"
	// TypeChoice 对应 Choice 问题。
	TypeChoice = "choice"
	// TypeScore 对应 Score 问题。
	TypeScore = "score"
)

// SystemOneResult 是一次 SystemOne 调用的完整结果。
type SystemOneResult struct {
	// Model 是实际处理请求的模型,例如 "jev-1.13.0"。
	Model string `json:"model"`
	// Answers 是按问题 id 索引的答案集合。
	Answers map[string]Answer `json:"answers"`
	// Usage 是本次调用的 token 用量。
	Usage Usage `json:"usage"`
}

// Usage 报告请求消耗的 token 数。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Answer 是单个问题的答案,三种原语共用此结构:
//
//   - Noul:   读 Noul 字段(0~1 的"是"概率)
//   - Choice: 读 Choice、Probabilities、Confidence
//   - Score:  读 Score、Legend、Probabilities、Confidence
type Answer struct {
	Type string `json:"type"`
	// Noul 是回答"是"的概率,仅 type == "noul" 时有意义。
	Noul float64 `json:"noul,omitempty"`
	// Choice 是概率最高的选项,仅 type == "choice" 时有意义。
	Choice string `json:"choice,omitempty"`
	// Score 是概率加权的等级位置(可落在两级之间),仅 type == "score" 时有意义。
	Score float64 `json:"score,omitempty"`
	// Probabilities 是完整概率分布:choice 映射到选项,score 映射到等级编号(字符串键)。
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	// Legend 是等级编号到描述的映射,仅 type == "score" 时返回。
	// 实际 API 返回的 value 可能是对象而非纯字符串,因此使用 any 类型以兼容。
	Legend map[string]any `json:"legend,omitempty"`
	// Confidence 是 0~1 的置信度,总结概率分布的集中程度,仅 choice 与 score 返回。
	// 注意它衡量分布集中度,不是整体流程的正确性许可。
	Confidence float64 `json:"confidence,omitempty"`
}

// Probability 返回指定选项(或等级编号)的概率。
func (a Answer) Probability(key string) (float64, bool) {
	p, ok := a.Probabilities[key]
	return p, ok
}

// NoulAnswer 是 Noul 问题的类型化答案。
type NoulAnswer struct {
	// Probability 是回答「是」的概率(0~1)。
	Probability float64
}

// ChoiceAnswer 是 Choice 问题的类型化答案。
type ChoiceAnswer struct {
	// Choice 是概率最高的选项。
	Choice string
	// Probabilities 是全部选项的概率分布。
	Probabilities map[string]float64
	// Confidence 是概率分布的集中程度(0~1)。
	Confidence float64
}

// ScoreAnswer 是 Score 问题的类型化答案。
type ScoreAnswer struct {
	// Score 是概率加权的等级位置(可落在两级之间)。
	Score float64
	// Legend 是等级编号到描述的映射。
	Legend map[string]any
	// Probabilities 是各等级的概率分布(键为等级编号字符串)。
	Probabilities map[string]float64
	// Confidence 是概率分布的集中程度(0~1)。
	Confidence float64
}

// AsNoul 在答案为 noul 类型时返回类型化结果,否则 ok 为 false。
// 相比直接读 Answer.Noul,它能在类型不匹配时显式失败而非静默返回零值。
func (a Answer) AsNoul() (NoulAnswer, bool) {
	if a.Type != TypeNoul {
		return NoulAnswer{}, false
	}
	return NoulAnswer{Probability: a.Noul}, true
}

// AsChoice 在答案为 choice 类型时返回类型化结果,否则 ok 为 false。
func (a Answer) AsChoice() (ChoiceAnswer, bool) {
	if a.Type != TypeChoice {
		return ChoiceAnswer{}, false
	}
	return ChoiceAnswer{
		Choice:        a.Choice,
		Probabilities: a.Probabilities,
		Confidence:    a.Confidence,
	}, true
}

// AsScore 在答案为 score 类型时返回类型化结果,否则 ok 为 false。
func (a Answer) AsScore() (ScoreAnswer, bool) {
	if a.Type != TypeScore {
		return ScoreAnswer{}, false
	}
	return ScoreAnswer{
		Score:         a.Score,
		Legend:        a.Legend,
		Probabilities: a.Probabilities,
		Confidence:    a.Confidence,
	}, true
}
