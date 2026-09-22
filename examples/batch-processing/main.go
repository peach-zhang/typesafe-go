// batch-processing 演示用 typesafe SDK 批量并发处理多条记录:
// 使用 goroutine + sync.WaitGroup 并发发送请求,
// context 控制整体超时,错误逐条收集不影响其他请求。
//
// 要点:
//   - Client 可安全地在多个 goroutine 间共享(文档明确保证)
//   - 每条记录独立的 context 超时,互不干扰
//   - 错误逐条收集,部分失败不影响整体
//   - 使用 WithLogger 开启 Info 级别日志观察并发行为
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/peach-zhang/typesafe-go"
)

// Record 是待分析的原始数据。
type Record struct {
	ID   string
	Text string
}

// Analysis 是单条分析结果。
type Analysis struct {
	ID       string
	Category string
	Toxicity float64
	Err      error
}

var records = []Record{
	{"rec-001", "The quarterly report shows a 15% increase in revenue across all regions."},
	{"rec-002", "This is absolutely unacceptable and I demand a refund immediately!"},
	{"rec-003", "Can you help me set up the API integration with our CRM system?"},
	{"rec-004", "Your product is garbage and your support team is useless."},
	{"rec-005", "We'd like to schedule a demo for our leadership team next week."},
}

func main() {
	// 开启日志观察并发请求行为
	client, err := typesafe.NewClient(
		typesafe.WithLogger(log.New(os.Stderr, "[typesafe] ", log.LstdFlags)),
		typesafe.WithLogLevel(typesafe.LogLevelInfo),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 整体超时 30 秒
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("开始批量处理 %d 条记录...\n\n", len(records))

	results := processBatch(ctx, client, records, 3) // 并发度 3

	fmt.Println("\n处理结果:")
	fmt.Println("========================================")

	var success, failed int
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("  [%s] ❌ 失败: %v\n", r.ID, r.Err)
			failed++
			continue
		}
		fmt.Printf("  [%s] 类别=%-12s 毒性=%.0f%%\n",
			r.ID, r.Category, r.Toxicity*100)
		success++
	}

	fmt.Printf("\n总计: %d 成功, %d 失败\n", success, failed)
}

// processBatch 并发处理一批记录,maxConcurrency 控制最大并发数。
func processBatch(ctx context.Context, client *typesafe.Client, records []Record, maxConcurrency int) []Analysis {
	results := make([]Analysis, len(records))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency) // 信号量控制并发度

	for i, rec := range records {
		wg.Add(1)
		sem <- struct{}{} // 获取信号量

		go func(idx int, r Record) {
			defer wg.Done()
			defer func() { <-sem }() // 释放信号量

			results[idx] = analyzeRecord(ctx, client, r)
		}(i, rec)
	}

	wg.Wait()
	return results
}

// analyzeRecord 分析单条记录。
func analyzeRecord(ctx context.Context, client *typesafe.Client, rec Record) Analysis {
	// 每条记录独立 10 秒超时
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: map[string]any{"text": rec.Text},
		Questions: typesafe.Questions{
			"category": typesafe.Choice(
				"What is the primary topic of `text`?",
				typesafe.ChoiceCriteria{
					"business":  "财务、报告、商业分析",
					"complaint": "投诉、不满、退款要求",
					"technical": "技术问题、API、集成",
					"sales":     "销售咨询、演示预约",
				},
			),
			"toxicity": typesafe.Noul(
				"Does `text` contain toxic, hostile, or abusive language?",
				&typesafe.NoulCriteria{
					True:  "人身攻击、辱骂、威胁性语言",
					False: "正常的情绪表达或建设性批评",
				},
			),
		},
	})
	if err != nil {
		return Analysis{ID: rec.ID, Err: err}
	}

	return Analysis{
		ID:       rec.ID,
		Category: result.Answers["category"].Choice,
		Toxicity: result.Answers["toxicity"].Noul,
	}
}
