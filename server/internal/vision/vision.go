// Package vision 契约 §6.1：LLM 识票。
//
//	POST /api/v1/tickets/recognize  { attachmentId } → 票据草稿（不落库）
//
// 流程：校验附件归属当前用户（user_id 隔离）→ 读磁盘图片 → 调 Claude 多模态
// （结构化输出，json_schema 约束）→ 归一化为草稿返回。
//
// 可用性与主流程解耦：未配置 PIAOJU_LLM_API_KEY 时 Routes 仍可挂载，
// 只在调用该端点时回 50001；服务照常起。
package vision

import (
	"strings"
	"time"
	"unicode/utf8"

	"piaoju/internal/platform/apperr"
)

// Draft 契约 §6.1 响应对象。字段语义同 §5 TicketInput 同名字段；
// 识别不出的字段回零值（""/0/{}），不猜。
type Draft struct {
	Kind        string            `json:"kind"`
	Title       string            `json:"title"`
	Venue       string            `json:"venue"`
	EventTime   string            `json:"eventTime"`
	Seat        string            `json:"seat"`
	Extra       map[string]string `json:"extra"`
	AmountCents int64             `json:"amountCents"`
	Confidence  float64           `json:"confidence"`
}

// extraWhitelist 契约 §5「extra 按 kind」白名单（与 ticket 包同源于 PROTOCOL.md §5；
// ticket 的同名表未导出，此处按契约重述，改契约时两处同步）。
var extraWhitelist = map[string][]string{
	"movie":      {"cinema", "hall", "filmFormat"},
	"show":       {"tour", "session", "zone"},
	"attraction": {"city", "ticketType"},
	"train":      {"trainNo", "fromStation", "toStation", "departTime", "arriveTime", "seatClass"},
	"flight":     {"flightNo", "airline", "fromAirport", "toAirport", "departTime", "arriveTime", "cabin"},
	"other":      {},
}

// 字段长度上限（对齐 ticket 包 / migrations 列宽，超长直接截断，避免草稿回填即被 40001 拒）。
const (
	maxTitleLen      = 128
	maxVenueLen      = 128
	maxSeatLen       = 64
	maxExtraValueLen = 512
)

// modelOutput LLM 结构化输出的落地结构（json_schema 保证形状，值仍需业务校验）。
// extra 是五种 kind 全字段的并集，按识别出的 kind 过滤后才进 Draft。
type modelOutput struct {
	Kind        string            `json:"kind"`
	Title       string            `json:"title"`
	Venue       string            `json:"venue"`
	EventTime   string            `json:"eventTime"`
	Seat        string            `json:"seat"`
	Extra       map[string]string `json:"extra"`
	AmountCents int64             `json:"amountCents"`
	Confidence  float64           `json:"confidence"`
}

// toDraft 校验 + 归一化模型输出。
// 非法 kind / 负数金额 → 50000（模型输出不可信，不吞不猜）；
// 无法解析的 eventTime、超长文本、不属于该 kind 的 extra 字段 → 静默归零/截断/丢弃。
func (m *modelOutput) toDraft() (*Draft, error) {
	kind := strings.TrimSpace(m.Kind)
	fields, ok := extraWhitelist[kind]
	if !ok {
		return nil, apperr.New(apperr.CodeInternal, "recognize: model returned invalid kind")
	}
	if m.AmountCents < 0 {
		return nil, apperr.New(apperr.CodeInternal, "recognize: model returned negative amount")
	}

	extra := make(map[string]string, len(fields))
	for _, f := range fields {
		extra[f] = truncate(strings.TrimSpace(m.Extra[f]), maxExtraValueLen)
	}

	conf := m.Confidence
	if conf < 0 {
		conf = 0
	} else if conf > 1 {
		conf = 1
	}

	return &Draft{
		Kind:        kind,
		Title:       truncate(strings.TrimSpace(m.Title), maxTitleLen),
		Venue:       truncate(strings.TrimSpace(m.Venue), maxVenueLen),
		EventTime:   normalizeTime(m.EventTime),
		Seat:        truncate(strings.TrimSpace(m.Seat), maxSeatLen),
		Extra:       extra,
		AmountCents: m.AmountCents,
		Confidence:  conf,
	}, nil
}

// normalizeTime 归一化为 RFC3339 UTC（conventions §1）；空/不可解析 → ""（不猜）。
func normalizeTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
