// Package ticket 契约 §5：票据 CRUD。
//
// 票与交易强关联：POST 在 DB 事务内同时建 transactions（direction 恒 expense）；
// PATCH 金额类字段同事务同步改交易；DELETE 同事务双软删。
package ticket

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/upload"
)

// 枚举（契约 §5 / conventions §1）。
var validKinds = map[string]bool{
	"movie": true, "show": true, "attraction": true,
	"train": true, "flight": true, "other": true,
}

var validPayments = map[string]bool{
	"wechat": true, "alipay": true, "cash": true, "card": true, "other": true,
}

// 字段长度上限（对齐 migrations/0003 列宽；memo 为 TEXT，做业务上限防滥用）。
const (
	maxTitleLen      = 128
	maxVenueLen      = 128
	maxSeatLen       = 64
	maxMemoLen       = 5000
	maxExtraValueLen = 512
	maxAttachments   = 20
	maxRating        = 5
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Ticket 契约 §5 响应对象（完整实体，POST/PATCH/GET 共用）。
type Ticket struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"`
	Title       string              `json:"title"`
	Venue       string              `json:"venue"`
	EventTime   string              `json:"eventTime"`
	Seat        string              `json:"seat"`
	Extra       map[string]string   `json:"extra"`
	Rating      int                 `json:"rating"`
	Memo        string              `json:"memo"`
	Transaction TxSummary           `json:"transaction"`
	Attachments []upload.Attachment `json:"attachments"`
	CreatedAt   string              `json:"createdAt"`
	UpdatedAt   string              `json:"updatedAt"`
}

// TxSummary 内嵌只读交易摘要（契约 §5，JOIN transactions 得来）。
type TxSummary struct {
	ID            string `json:"id"`
	AmountCents   int64  `json:"amountCents"`
	CategoryID    int64  `json:"categoryId"`
	PaymentMethod string `json:"paymentMethod"`
}

// body TicketInput / Partial<TicketInput> 共用解码结构：指针区分「未提供」与零值。
// updatedAt 非契约 TicketInput 字段，作为幂等 LWW 提示可选接受（对齐 sync 的 clientUpdatedAt 语义）。
type body struct {
	ID            *string        `json:"id"`
	TransactionID *string        `json:"transactionId"`
	Kind          *string        `json:"kind"`
	Title         *string        `json:"title"`
	Venue         *string        `json:"venue"`
	EventTime     *string        `json:"eventTime"`
	Seat          *string        `json:"seat"`
	Extra         map[string]any `json:"extra"`
	Rating        *int           `json:"rating"`
	Memo          *string        `json:"memo"`
	AmountCents   *int64         `json:"amountCents"`
	CategoryID    *int64         `json:"categoryId"`
	PaymentMethod *string        `json:"paymentMethod"`
	OccurredAt    *string        `json:"occurredAt"`
	AttachmentIDs *[]int64       `json:"attachmentIds"`
	UpdatedAt     *string        `json:"updatedAt"`
}

// createData 校验通过、补全默认值后的 POST 载荷。
type createData struct {
	ID            string
	TransactionID string // 联动交易主键，客户端 UUID（契约 §5 v1.2）；重放时以库中已有值为准
	Kind          string
	Title         string
	Venue         string
	Seat          string
	Memo          string
	EventTime     time.Time
	Extra         map[string]string
	Rating        int
	AmountCents   int64
	CategoryID    int64
	PaymentMethod string
	OccurredAt    time.Time
	AttachmentIDs []int64
	ClientUpdated *time.Time // 可选 LWW 提示；比服务端 updated_at 旧 → 40902
}

// patchData 校验通过的 PATCH 载荷；nil 指针 = 未提供。
type patchData struct {
	Kind          *string
	Title         *string
	Venue         *string
	Seat          *string
	Memo          *string
	EventTime     *time.Time
	Extra         map[string]any // nil = 未提供；具体校验需结合生效 kind，在 service 内做
	Rating        *int
	AmountCents   *int64
	CategoryID    *int64
	PaymentMethod *string
	OccurredAt    *time.Time
	AttachmentIDs *[]int64
}

func badParam(format string, a ...any) error {
	return apperr.New(apperr.CodeInvalidParam, fmt.Sprintf(format, a...))
}

func badEnum(format string, a ...any) error {
	return apperr.New(apperr.CodeUnsupportedEnum, fmt.Sprintf(format, a...))
}

// parseRFC3339 契约时间格式：RFC3339；统一转 UTC、截断到毫秒（DATETIME(3) 精度）。
func parseRFC3339(s, field string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, badParam("%s must be RFC3339 (e.g. 2026-07-12T11:30:00Z)", field)
	}
	return t.UTC().Truncate(time.Millisecond), nil
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parseCreate 校验 POST 载荷（TicketInput）。
// 必填：id/kind/title/eventTime/amountCents/categoryId/occurredAt；
// 缺省：venue/seat/memo=""、rating=0、extra={}、paymentMethod="other"、attachmentIds=[]。
func parseCreate(in body) (createData, error) {
	var d createData

	if in.ID == nil {
		return d, badParam("id is required (client-generated UUID)")
	}
	id := strings.ToLower(strings.TrimSpace(*in.ID))
	if !uuidRe.MatchString(id) {
		return d, badParam("id must be a UUID")
	}
	d.ID = id

	// transactionId：联动交易主键，同样客户端生成（契约 §5 v1.2，离线建票需立刻入本地账本）。
	if in.TransactionID == nil {
		return d, badParam("transactionId is required (client-generated UUID)")
	}
	txID := strings.ToLower(strings.TrimSpace(*in.TransactionID))
	if !uuidRe.MatchString(txID) {
		return d, badParam("transactionId must be a UUID")
	}
	if txID == id {
		return d, badParam("transactionId must differ from ticket id")
	}
	d.TransactionID = txID

	if in.Kind == nil {
		return d, badParam("kind is required")
	}
	if !validKinds[*in.Kind] {
		return d, badEnum("unsupported kind %q", *in.Kind)
	}
	d.Kind = *in.Kind

	if in.Title == nil {
		return d, badParam("title is required")
	}
	d.Title = strings.TrimSpace(*in.Title)
	if d.Title == "" {
		return d, badParam("title must not be empty")
	}
	if utf8.RuneCountInString(d.Title) > maxTitleLen {
		return d, badParam("title too long (max %d chars)", maxTitleLen)
	}

	if in.Venue != nil {
		if utf8.RuneCountInString(*in.Venue) > maxVenueLen {
			return d, badParam("venue too long (max %d chars)", maxVenueLen)
		}
		d.Venue = *in.Venue
	}
	if in.Seat != nil {
		if utf8.RuneCountInString(*in.Seat) > maxSeatLen {
			return d, badParam("seat too long (max %d chars)", maxSeatLen)
		}
		d.Seat = *in.Seat
	}
	if in.Memo != nil {
		if utf8.RuneCountInString(*in.Memo) > maxMemoLen {
			return d, badParam("memo too long (max %d chars)", maxMemoLen)
		}
		d.Memo = *in.Memo
	}

	if in.EventTime == nil {
		return d, badParam("eventTime is required")
	}
	et, err := parseRFC3339(*in.EventTime, "eventTime")
	if err != nil {
		return d, err
	}
	d.EventTime = et

	if in.Rating != nil {
		if *in.Rating < 0 || *in.Rating > maxRating {
			return d, badParam("rating must be within 0-%d", maxRating)
		}
		d.Rating = *in.Rating
	}

	extra, err := normalizeExtra(d.Kind, in.Extra)
	if err != nil {
		return d, err
	}
	d.Extra = extra

	if in.AmountCents == nil {
		return d, badParam("amountCents is required")
	}
	if *in.AmountCents < 0 {
		return d, badParam("amountCents must be >= 0")
	}
	d.AmountCents = *in.AmountCents

	if in.CategoryID == nil {
		return d, badParam("categoryId is required")
	}
	if *in.CategoryID < 1 {
		return d, badParam("categoryId must be a positive id")
	}
	d.CategoryID = *in.CategoryID

	d.PaymentMethod = "other"
	if in.PaymentMethod != nil {
		if !validPayments[*in.PaymentMethod] {
			return d, badEnum("unsupported paymentMethod %q", *in.PaymentMethod)
		}
		d.PaymentMethod = *in.PaymentMethod
	}

	if in.OccurredAt == nil {
		return d, badParam("occurredAt is required")
	}
	ot, err := parseRFC3339(*in.OccurredAt, "occurredAt")
	if err != nil {
		return d, err
	}
	d.OccurredAt = ot

	if in.AttachmentIDs != nil {
		ids, err := normalizeAttachmentIDs(*in.AttachmentIDs)
		if err != nil {
			return d, err
		}
		d.AttachmentIDs = ids
	}

	if in.UpdatedAt != nil {
		ut, err := parseRFC3339(*in.UpdatedAt, "updatedAt")
		if err != nil {
			return d, err
		}
		d.ClientUpdated = &ut
	}
	return d, nil
}

// parsePatch 校验 Partial<TicketInput>；body 内 id 出现时必须与路径 id 一致。
func parsePatch(pathID string, in body) (patchData, error) {
	var p patchData

	if in.ID != nil && strings.ToLower(strings.TrimSpace(*in.ID)) != pathID {
		return p, badParam("body id must match path id")
	}
	if in.Kind != nil {
		if !validKinds[*in.Kind] {
			return p, badEnum("unsupported kind %q", *in.Kind)
		}
		p.Kind = in.Kind
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return p, badParam("title must not be empty")
		}
		if utf8.RuneCountInString(t) > maxTitleLen {
			return p, badParam("title too long (max %d chars)", maxTitleLen)
		}
		p.Title = &t
	}
	if in.Venue != nil {
		if utf8.RuneCountInString(*in.Venue) > maxVenueLen {
			return p, badParam("venue too long (max %d chars)", maxVenueLen)
		}
		p.Venue = in.Venue
	}
	if in.Seat != nil {
		if utf8.RuneCountInString(*in.Seat) > maxSeatLen {
			return p, badParam("seat too long (max %d chars)", maxSeatLen)
		}
		p.Seat = in.Seat
	}
	if in.Memo != nil {
		if utf8.RuneCountInString(*in.Memo) > maxMemoLen {
			return p, badParam("memo too long (max %d chars)", maxMemoLen)
		}
		p.Memo = in.Memo
	}
	if in.EventTime != nil {
		et, err := parseRFC3339(*in.EventTime, "eventTime")
		if err != nil {
			return p, err
		}
		p.EventTime = &et
	}
	p.Extra = in.Extra // 结合生效 kind 在 service 内校验（可能依赖 DB 现值）
	if in.Rating != nil {
		if *in.Rating < 0 || *in.Rating > maxRating {
			return p, badParam("rating must be within 0-%d", maxRating)
		}
		p.Rating = in.Rating
	}
	if in.AmountCents != nil {
		if *in.AmountCents < 0 {
			return p, badParam("amountCents must be >= 0")
		}
		p.AmountCents = in.AmountCents
	}
	if in.CategoryID != nil {
		if *in.CategoryID < 1 {
			return p, badParam("categoryId must be a positive id")
		}
		p.CategoryID = in.CategoryID
	}
	if in.PaymentMethod != nil {
		if !validPayments[*in.PaymentMethod] {
			return p, badEnum("unsupported paymentMethod %q", *in.PaymentMethod)
		}
		p.PaymentMethod = in.PaymentMethod
	}
	if in.OccurredAt != nil {
		ot, err := parseRFC3339(*in.OccurredAt, "occurredAt")
		if err != nil {
			return p, err
		}
		p.OccurredAt = &ot
	}
	if in.AttachmentIDs != nil {
		ids, err := normalizeAttachmentIDs(*in.AttachmentIDs)
		if err != nil {
			return p, err
		}
		p.AttachmentIDs = &ids
	}
	return p, nil
}

// normalizeAttachmentIDs 去重保序；非正 id / 超量 → 40001。
func normalizeAttachmentIDs(ids []int64) ([]int64, error) {
	if len(ids) > maxAttachments {
		return nil, badParam("too many attachments (max %d)", maxAttachments)
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id < 1 {
			return nil, badParam("attachmentIds must be positive ids")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}
