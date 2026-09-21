package typesafe

import "fmt"

// Questions 是问题 id 到问题的映射。id 只供代码使用,不会发送给模型,
// 响应中的答案以相同的 id 返回。
type Questions map[string]Question

// Question 是一个待评估的类型化问题,只能通过 Noul、Choice、Score 构造。
type Question interface {
	isQuestion()
	// validate 在请求前校验本问题的约束,返回违反约束的说明。
	validate() error
}

// NoulCriteria 分别描述 true / false 两种极性的含义,帮助模型校准判断。
type NoulCriteria struct {
	// True 描述答案为"是"时的典型情形。
	True any `json:"true,omitempty"`
	// False 描述答案为"否"时的典型情形。
	False any `json:"false,omitempty"`
}

// ChoiceCriteria 把每个选项标签映射到它的说明。
// 说明可以是字符串或 JSON 对象;nil 表示该标签无额外描述。
type ChoiceCriteria map[string]any

// ScoreCriteria 是有序的等级描述,至少 2 级、最多 10 级,
// 每一级都应描述具体情形并可独立理解。
type ScoreCriteria []any

// Noul 构造一个是否问题,答案返回"是"的概率(0~1)。
// instructions 可以是字符串、JSON 对象或数组;criteria 可为 nil。
//
// 若干标签可能同时适用时,应为每个标签各建一个 Noul 问题,而不是用 Choice。
func Noul(instructions any, criteria *NoulCriteria) Question {
	return noulQuestion{Type: "noul", Instructions: instructions, Criteria: criteria}
}

// Choice 构造一个单选题:模型从 criteria 定义的选项集合中选出概率最高的一个,
// 并返回全部选项的概率分布。最多支持 255 个选项。
func Choice(instructions any, criteria ChoiceCriteria) Question {
	return choiceQuestion{Type: "choice", Instructions: instructions, Criteria: criteria}
}

// Score 构造一个等级评分题:沿 criteria 描述的有序等级评估程度,
// score 可落在两级之间(概率加权)。
func Score(instructions any, criteria ScoreCriteria) Question {
	return scoreQuestion{Type: "score", Instructions: instructions, Criteria: criteria}
}

type noulQuestion struct {
	Type         string        `json:"type"`
	Instructions any           `json:"instructions"`
	Criteria     *NoulCriteria `json:"criteria,omitempty"`
}

type choiceQuestion struct {
	Type         string         `json:"type"`
	Instructions any            `json:"instructions"`
	Criteria     ChoiceCriteria `json:"criteria"`
}

type scoreQuestion struct {
	Type         string        `json:"type"`
	Instructions any           `json:"instructions"`
	Criteria     ScoreCriteria `json:"criteria"`
}

func (noulQuestion) isQuestion()   {}
func (choiceQuestion) isQuestion() {}
func (scoreQuestion) isQuestion()  {}

func (q noulQuestion) validate() error {
	if q.Instructions == nil {
		return fmt.Errorf("noul 问题缺少 instructions")
	}
	return nil
}

func (q choiceQuestion) validate() error {
	if q.Instructions == nil {
		return fmt.Errorf("choice 问题缺少 instructions")
	}
	if len(q.Criteria) == 0 {
		return fmt.Errorf("choice 问题缺少 criteria(选项集合)")
	}
	if len(q.Criteria) > 255 {
		return fmt.Errorf("choice 问题的选项数 %d 超过上限 255", len(q.Criteria))
	}
	return nil
}

func (q scoreQuestion) validate() error {
	if q.Instructions == nil {
		return fmt.Errorf("score 问题缺少 instructions")
	}
	if len(q.Criteria) < 2 {
		return fmt.Errorf("score 问题的 criteria 至少需要 2 个等级,实际 %d 个", len(q.Criteria))
	}
	if len(q.Criteria) > 10 {
		return fmt.Errorf("score 问题的等级数 %d 超过上限 10", len(q.Criteria))
	}
	return nil
}
