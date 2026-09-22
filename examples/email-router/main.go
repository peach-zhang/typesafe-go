// email-router 演示用 typesafe SDK 做邮件智能路由:
// 一次调用同时判断部门(Choice)、紧急度(Noul)和优先级(Score),
// 然后利用概率分布做更精细的路由决策。
//
// 要点:
//   - Choice 返回完整概率分布,不只是最高概率选项
//   - 当最高和次高概率接近时,置信度低 → 走人工
//   - 代码侧组合三种问题原语的结果做最终决策
package main

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/peach-zhang/typesafe-go"
)

var emails = []struct {
	subject string
	body    string
}{
	{
		subject: "Invoice #4521 discrepancy",
		body:    "Hi, we were charged $2,400 but the contract says $2,000. Can you check?",
	},
	{
		subject: "API integration returning 500",
		body:    "Our production system has been failing since 3 AM. The /v2/orders endpoint returns 500 for every request. This is critical.",
	},
	{
		subject: "Enterprise plan pricing",
		body:    "We're a 500-person company exploring your platform. Could we schedule a call to discuss custom pricing and SLA options?",
	},
	{
		subject: "Cannot reset my password",
		body:    "I've tried resetting my password 5 times but never receive the email. I need access before our board meeting tomorrow.",
	},
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	for _, email := range emails {
		route(ctx, client, email.subject, email.body)
	}
}

func route(ctx context.Context, client *typesafe.Client, subject, body string) {
	result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: map[string]any{
			"email": map[string]any{
				"subject": subject,
				"body":    body,
			},
		},
		Questions: typesafe.Questions{
			// Choice: 部门分类 — 返回概率最高的选项 + 完整概率分布
			"department": typesafe.Choice(
				"Which department should handle `email`?",
				typesafe.ChoiceCriteria{
					"billing":   "Payments, invoicing, refunds, pricing disputes",
					"technical": "Bugs, outages, API errors, integration issues",
					"sales":     "New accounts, pricing inquiries, enterprise plans, demos",
					"support":   "Account access, password issues, how-to questions",
				},
			),
			// Noul: 紧急度判断
			"is_urgent": typesafe.Noul(
				"Does `email` describe a time-sensitive issue requiring immediate attention?",
				&typesafe.NoulCriteria{
					True:  "生产环境故障、明确的时间期限、资金正在损失",
					False: "常规咨询、信息查询、非紧急的账户问题",
				},
			),
			// Score: 客户情绪 — 可落在两级之间
			"sentiment": typesafe.Score(
				"How would you rate the emotional tone of `email`?",
				typesafe.ScoreCriteria{
					"平静友好", "略有疑虑", "不满焦虑", "焦急迫切", "非常愤怒",
				},
			),
		},
	})
	if err != nil {
		log.Printf("[%s] %v", subject, err)
		return
	}

	dept := result.Answers["department"]
	urgent := result.Answers["is_urgent"]
	sentiment := result.Answers["sentiment"]

	fmt.Printf("📧 主题: %s\n", subject)
	fmt.Printf("   摘要: %s\n", body)

	// 展示概率分布 — 不只是看最高概率选项
	fmt.Printf("   部门: %s(置信度 %.0f%%)\n", dept.Choice, dept.Confidence*100)
	fmt.Printf("   概率分布:")
	sorted := sortedProbabilities(dept.Probabilities)
	for _, kv := range sorted {
		fmt.Printf(" %s=%.0f%%", kv.key, kv.val*100)
	}
	fmt.Println()

	fmt.Printf("   紧急度: %.0f%%  情绪: %.1f/4\n", urgent.Noul*100, sentiment.Score)

	// 路由决策:
	// 1. 置信度不足(< 0.7) → 人工分配
	// 2. 生产事故 + 高情绪 → 最高优先级
	// 3. 正常路由
	switch {
	case dept.Confidence < 0.7:
		fmt.Println("   → 人工分配(分类不确定)")
	case dept.Choice == "technical" && urgent.Noul > 0.7 && sentiment.Score >= 3:
		fmt.Println("   → 🔴 P0 升级(生产事故 + 客户情绪激动)")
	case urgent.Noul > 0.7:
		fmt.Printf("   → ⚡ 加急处理 → %s 组\n", dept.Choice)
	default:
		fmt.Printf("   → 常规路由 → %s 组\n", dept.Choice)
	}
	fmt.Printf("   用量: %d/%d token, 模型 %s\n\n",
		result.Usage.InputTokens, result.Usage.OutputTokens, result.Model)
}

type kv struct {
	key string
	val float64
}

func sortedProbabilities(m map[string]float64) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].val > out[j].val })
	return out
}
