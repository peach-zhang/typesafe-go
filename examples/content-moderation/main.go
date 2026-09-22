// content-moderation 演示用 typesafe SDK 做内容安全审核:
// 多个 Noul 维度并行判断(暴力、色情、仇恨言论、诈骗),
// 再用代码侧的组合逻辑决定通过、拦截或降级人工复核。
//
// 要点:
//   - 多个 Noul 问题在一次调用中并行评估,零额外延迟
//   - 每个 Noul 都附带 criteria 帮助模型校准判断
//   - 低概率"灰色地带"自动降级到人工复核
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/peach-zhang/typesafe-go"
)

var comments = []string{
	"我是秦始皇,V我五十元,我封你做将军.",
	"周末一起去爬山吗？天气看起来不错 😊",
	"我要找到你然后让你付出代价,你等着瞧",
	"加我微信转账返现 200%,限时活动先到先得",
	"这部电影的特效做得还行,但剧情太老套了",
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	for _, comment := range comments {
		moderate(ctx, client, comment)
	}
}

func moderate(ctx context.Context, client *typesafe.Client, text string) {
	result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		// 只传入待审核文本,问题中用反引号引用
		State: map[string]any{"comment": text},
		Questions: typesafe.Questions{
			"is_violent": typesafe.Noul("Does `comment` contain threats, violence, or physical harm?", &typesafe.NoulCriteria{
				True:  "明确的暴力威胁、人身攻击、伤害意图",
				False: "普通的情绪表达或不满",
			}),
			"is_hateful": typesafe.Noul("Does `comment` contain hate speech, discrimination, or harassment targeting a person or group?", &typesafe.NoulCriteria{
				True:  "针对个人/群体的侮辱、歧视、恶意人身攻击",
				False: "对事不对人的批评或负面评价",
			}),
			"is_fraud": typesafe.Noul("Does `comment` appear to be a scam, phishing, or fraudulent promotion?", &typesafe.NoulCriteria{
				True:  "诱导转账、虚假承诺返利、钓鱼链接、仿冒身份",
				False: "正常的商品推荐或讨论",
			}),
			"is_spam": typesafe.Noul("Is `comment` irrelevant spam, advertising, or repetitive low-quality content?", &typesafe.NoulCriteria{
				True:  "无意义的广告刷屏、重复发布、与话题无关的推销",
				False: "有实质内容的用户发言",
			}),
		},
	})
	if err != nil {
		log.Printf("%q: %v", text, err)
		return
	}

	violent := result.Answers["is_violent"]
	hateful := result.Answers["is_hateful"]
	fraud := result.Answers["is_fraud"]
	spam := result.Answers["is_spam"]

	fmt.Printf("评论: %s\n", text)
	fmt.Printf("  暴力: %.0f%%  仇恨: %.0f%%  诈骗: %.0f%%  垃圾: %.0f%%\n",
		violent.Noul*100, hateful.Noul*100, fraud.Noul*100, spam.Noul*100)

	// 代码侧决策逻辑 —— 模型只负责判断,路由规则由代码掌控
	switch {
	case violent.Noul >= 0.8 || hateful.Noul >= 0.8:
		fmt.Println("  → 决定: 拦截(高风险内容)")
	case fraud.Noul >= 0.7:
		fmt.Println("  → 决定: 拦截(疑似诈骗)")
	case violent.Noul >= 0.3 || hateful.Noul >= 0.3 || fraud.Noul >= 0.3:
		fmt.Println("  → 决定: 降级人工复核(灰色地带)")
	case spam.Noul >= 0.6:
		fmt.Println("  → 决定: 折叠(垃圾内容)")
	default:
		fmt.Println("  → 决定: 通过")
	}

	fmt.Printf("  用量: %d 输入 / %d 输出 token,模型 %s\n\n",
		result.Usage.InputTokens, result.Usage.OutputTokens, result.Model)
}
