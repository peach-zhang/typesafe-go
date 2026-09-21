// triage 演示用 typesafe SDK 做工单分类:一次调用并行评估类别、紧急度
// 与客户情绪,再由代码组合结果决定路由——模型提供判断,代码掌控流程。
package main

import (
	"context"
	"fmt"
	"log"

	"jev/typesafe"
)

var tickets = []string{
	"Help! My payouts have been failing for 3 days.",
	"Quick question: do you offer volume discounts for annual plans?",
}

func main() {
	client, err := typesafe.NewClient() // API key 来自环境变量 TYPESAFE_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// 演示 Models 端点:列出账号可用的模型
	if cards, err := client.Models(ctx); err == nil {
		fmt.Print("可用模型:")
		for _, m := range cards {
			fmt.Printf(" %s(发布于 %s)", m.Name, m.ReleaseDate)
		}
		fmt.Println()
	}

	for _, ticket := range tickets {
		triage(ctx, client, ticket)
	}
}

func triage(ctx context.Context, client *typesafe.Client, document string) {
	result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		// 只放与当前问题相关的状态,问题里用反引号路径引用
		State: map[string]any{"ticket": map[string]any{"message": document}},
		Questions: typesafe.Questions{
			"category": typesafe.Choice(
				"Which team should handle the issue in `ticket.message`?",
				typesafe.ChoiceCriteria{
					"billing":   "Payments, invoicing, refunds, duplicate charges",
					"technical": "Bugs, outages, integrations",
					"sales":     "Pricing, upgrades, new accounts",
				},
			),
			"is_urgent": typesafe.Noul("Does `ticket.message` convey urgency?", &typesafe.NoulCriteria{
				True:  "明确的时间敏感性:故障持续、最后期限、资金受影响",
				False: "常规咨询,没有时间压力",
			}),
			"frustration": typesafe.Score("How frustrated is the customer in `ticket.message`?",
				typesafe.ScoreCriteria{"平静", "略感不满", "沮丧", "非常愤怒"},
			),
		},
	})
	if err != nil {
		log.Printf("%q: %v", document, err)
		return
	}

	category := result.Answers["category"]
	urgency := result.Answers["is_urgent"]
	frustration := result.Answers["frustration"]
	maxLevel := float64(len(frustration.Legend) - 1)

	fmt.Printf("工单: %s\n", document)
	fmt.Printf("  类别:   %s(置信度 %.2f)\n", category.Choice, category.Confidence)
	fmt.Printf("  紧急:   %.0f%%\n", urgency.Noul*100)
	fmt.Printf("  情绪:   %.1f / %.0f 级(置信度 %.2f)\n", frustration.Score, maxLevel, frustration.Confidence)

	// 组合逻辑完全在代码侧:置信度不足或情绪极端时升级人工
	switch {
	case category.Confidence < 0.75:
		fmt.Println("  → 路由: 人工复核(分类置信度不足)")
	case frustration.Score >= maxLevel-0.5:
		fmt.Println("  → 路由: 优先处理(客户情绪激烈)")
	default:
		fmt.Printf("  → 路由: %s 组\n", category.Choice)
	}
	fmt.Printf("  用量:   %d 输入 / %d 输出 token,模型 %s\n\n",
		result.Usage.InputTokens, result.Usage.OutputTokens, result.Model)
}
