package vision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"piaoju/internal/platform/apperr"
)

// recognizer LLM 抽象：service 只依赖它，单测注入 fake，不打真 API。
type recognizer interface {
	recognize(ctx context.Context, mediaType string, image []byte) (*modelOutput, error)
}

// claudeClient 官方 SDK（github.com/anthropics/anthropic-sdk-go）多模态调用：
// base64 image block + text block，结构化输出（output_config.format = json_schema）约束返回。
type claudeClient struct {
	api   anthropic.Client
	model anthropic.Model
}

const (
	llmModel     = anthropic.ModelClaudeOpus4_8
	llmMaxTokens = 2048
	llmTimeout   = 60 * time.Second
)

func newClaudeClient(apiKey string) *claudeClient {
	return &claudeClient{
		api: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithRequestTimeout(llmTimeout),
		),
		model: llmModel,
	}
}

const systemPrompt = `你是票据识别助手。用户给你一张票据照片（电影票/演出票/景点门票/火车票/登机牌/其他小票）。
请只依据图片上可见的信息，抽取结构化票据草稿。

规则（务必严格遵守）：
1. 先判断票种 kind，只能是：movie / show / attraction / train / flight / other。无法归类时用 other。
2. extra 是所有票种字段的并集，只填与判定的 kind 相符的字段，其余一律填空字符串 ""：
   - movie: cinema(影院) hall(影厅) filmFormat(IMAX/杜比/2D…)
   - show: tour(巡演名) session(场次) zone(看台/区域)
   - attraction: city(城市) ticketType(成人/学生/儿童…)
   - train: trainNo fromStation toStation departTime arriveTime seatClass
   - flight: flightNo airline fromAirport toAirport departTime arriveTime cabin
   - other: 无字段，全部留空
3. amountCents 是整数「分」（例如 ¥68.50 → 6850）。图片上没有金额时填 0，绝不估算。
4. eventTime 用 RFC3339 UTC（如 2026-07-12T11:30:00Z）。票面通常是北京时间（UTC+8），换算成 UTC。
   只有年月日没有时刻时，用当天 00:00 北京时间换算。完全没有时间信息时填空字符串 ""。
5. 任何看不清、不存在、需要猜测的字段，一律填零值（字符串 ""、数字 0），严禁编造。
6. confidence 是 0~1 的整体置信度：图片清晰且关键字段（票种/标题/时间/金额）都读到 → 接近 1；
   大量字段读不出或图片模糊 → 低于 0.6。`

const userPrompt = "识别这张票据，按 schema 输出结构化草稿。"

// outputSchema json_schema：五种 kind 的 extra 白名单并集（契约 §5），全部 required + 禁止额外字段。
func outputSchema() map[string]any {
	extraProps := map[string]any{}
	var extraRequired []string
	for _, fields := range extraWhitelist {
		for _, f := range fields {
			if _, ok := extraProps[f]; ok {
				continue
			}
			extraProps[f] = map[string]any{"type": "string"}
			extraRequired = append(extraRequired, f)
		}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": map[string]any{
				"type": "string",
				"enum": []string{"movie", "show", "attraction", "train", "flight", "other"},
			},
			"title":     map[string]any{"type": "string", "description": "票据标题（影片名/演出名/景点名/车次或航班的行程描述）"},
			"venue":     map[string]any{"type": "string", "description": "场馆/地点"},
			"eventTime": map[string]any{"type": "string", "description": "RFC3339 UTC，未知填空串"},
			"seat":      map[string]any{"type": "string", "description": "座位号"},
			"extra": map[string]any{
				"type":                 "object",
				"properties":           extraProps,
				"required":             extraRequired,
				"additionalProperties": false,
			},
			"amountCents": map[string]any{"type": "integer", "description": "金额，整数分；未知填 0"},
			"confidence":  map[string]any{"type": "number", "description": "0~1 的整体置信度"},
		},
		"required":             []string{"kind", "title", "venue", "eventTime", "seat", "extra", "amountCents", "confidence"},
		"additionalProperties": false,
	}
}

func (c *claudeClient) recognize(ctx context.Context, mediaType string, image []byte) (*modelOutput, error) {
	b64 := base64.StdEncoding.EncodeToString(image)

	resp, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: llmMaxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: outputSchema()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewImageBlockBase64(mediaType, b64),
				anthropic.NewTextBlock(userPrompt),
			),
		},
	})
	if err != nil {
		return nil, mapLLMError(err)
	}

	// 结构化输出保证首个 text block 是符合 schema 的 JSON。
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			var out modelOutput
			if err := json.Unmarshal([]byte(t.Text), &out); err != nil {
				return nil, apperr.New(apperr.CodeInternal, "recognize: malformed model output")
			}
			return &out, nil
		}
	}
	return nil, apperr.New(apperr.CodeInternal, "recognize: empty model output")
}

// mapLLMError 上游限流/超额（429）与过载（529）→ 契约 42901；其余归 50000（内部细节不外泄）。
func mapLLMError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429, 529:
			return apperr.New(codeRateLimited, "recognize service is rate limited, retry later")
		}
		return fmt.Errorf("vision: llm call failed (status %d): %w", apiErr.StatusCode, err)
	}
	return fmt.Errorf("vision: llm call failed: %w", err)
}
