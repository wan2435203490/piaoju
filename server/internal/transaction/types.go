// Package transaction 契约 §4：交易 CRUD（记账核心）。
//
// id 为客户端生成 UUIDv4，POST 幂等 upsert（LWW，携带更旧 updatedAt → 40902）；
// direction 建后不可改（PATCH/重放改向 → 40001）；ticketId 是从 tickets 表派生的
// 只读反向关联——票↔交易联动写由 ticket 模块负责，本模块对 tickets 只读。
package transaction

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"piaoju/internal/platform/apperr"
)

// 枚举（契约 §4 / conventions §1）。
var validDirections = map[string]bool{"expense": true, "income": true}

var validPayments = map[string]bool{
	"wechat": true, "alipay": true, "cash": true, "card": true, "other": true,
}

// maxNoteLen 对齐 migrations/0003 note VARCHAR(500) 列宽。
const maxNoteLen = 500

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Transaction 契约 §4 响应对象（完整实体，POST/PATCH/列表共用）。
type Transaction struct {
	ID            string  `json:"id"`
	AmountCents   int64   `json:"amountCents"`
	Direction     string  `json:"direction"`
	CategoryID    int64   `json:"categoryId"`
	Note          string  `json:"note"`
	OccurredAt    string  `json:"occurredAt"`
	PaymentMethod string  `json:"paymentMethod"`
	TicketID      *string `json:"ticketId"` // 反向关联，只读（tickets 表派生）
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

// body TransactionInput / Partial<TransactionInput> 共用解码结构：指针区分「未提供」与零值。
// updatedAt 非契约 TransactionInput 字段，作为幂等 LWW 提示可选接受（对齐 ticket/sync 的
// clientUpdatedAt 语义）。
type body struct {
	ID            *string `json:"id"`
	AmountCents   *int64  `json:"amountCents"`
	Direction     *string `json:"direction"`
	CategoryID    *int64  `json:"categoryId"`
	Note          *string `json:"note"`
	OccurredAt    *string `json:"occurredAt"`
	PaymentMethod *string `json:"paymentMethod"`
	UpdatedAt     *string `json:"updatedAt"`
}

// createData 校验通过、补全默认值后的 POST 载荷。
type createData struct {
	ID            string
	AmountCents   int64
	Direction     string
	CategoryID    int64
	Note          string
	OccurredAt    time.Time
	PaymentMethod string
	ClientUpdated *time.Time // 可选 LWW 提示；比服务端 updated_at 旧 → 40902
}

// patchData 校验通过的 PATCH 载荷；nil 指针 = 未提供。
// Direction 需结合 DB 现值比对（与现值不同 → 40001），在 service 内做。
type patchData struct {
	AmountCents   *int64
	Direction     *string
	CategoryID    *int64
	Note          *string
	OccurredAt    *time.Time
	PaymentMethod *string
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

// parseCreate 校验 POST 载荷（TransactionInput）。
// 必填：id/amountCents/direction/categoryId/occurredAt；缺省：note=""、paymentMethod="other"。
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

	if in.AmountCents == nil {
		return d, badParam("amountCents is required")
	}
	if *in.AmountCents < 0 {
		return d, badParam("amountCents must be >= 0")
	}
	d.AmountCents = *in.AmountCents

	if in.Direction == nil {
		return d, badParam("direction is required")
	}
	if !validDirections[*in.Direction] {
		return d, badEnum("unsupported direction %q", *in.Direction)
	}
	d.Direction = *in.Direction

	if in.CategoryID == nil {
		return d, badParam("categoryId is required")
	}
	if *in.CategoryID < 1 {
		return d, badParam("categoryId must be a positive id")
	}
	d.CategoryID = *in.CategoryID

	if in.Note != nil {
		if utf8.RuneCountInString(*in.Note) > maxNoteLen {
			return d, badParam("note too long (max %d chars)", maxNoteLen)
		}
		d.Note = *in.Note
	}

	if in.OccurredAt == nil {
		return d, badParam("occurredAt is required")
	}
	ot, err := parseRFC3339(*in.OccurredAt, "occurredAt")
	if err != nil {
		return d, err
	}
	d.OccurredAt = ot

	d.PaymentMethod = "other"
	if in.PaymentMethod != nil {
		if !validPayments[*in.PaymentMethod] {
			return d, badEnum("unsupported paymentMethod %q", *in.PaymentMethod)
		}
		d.PaymentMethod = *in.PaymentMethod
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

// parsePatch 校验 Partial<TransactionInput>；body 内 id 出现时必须与路径 id 一致。
func parsePatch(pathID string, in body) (patchData, error) {
	var p patchData

	if in.ID != nil && strings.ToLower(strings.TrimSpace(*in.ID)) != pathID {
		return p, badParam("body id must match path id")
	}
	if in.AmountCents != nil {
		if *in.AmountCents < 0 {
			return p, badParam("amountCents must be >= 0")
		}
		p.AmountCents = in.AmountCents
	}
	if in.Direction != nil {
		if !validDirections[*in.Direction] {
			return p, badEnum("unsupported direction %q", *in.Direction)
		}
		p.Direction = in.Direction // 与 DB 现值不同 → 40001，在 service 内比对
	}
	if in.CategoryID != nil {
		if *in.CategoryID < 1 {
			return p, badParam("categoryId must be a positive id")
		}
		p.CategoryID = in.CategoryID
	}
	if in.Note != nil {
		if utf8.RuneCountInString(*in.Note) > maxNoteLen {
			return p, badParam("note too long (max %d chars)", maxNoteLen)
		}
		p.Note = in.Note
	}
	if in.OccurredAt != nil {
		ot, err := parseRFC3339(*in.OccurredAt, "occurredAt")
		if err != nil {
			return p, err
		}
		p.OccurredAt = &ot
	}
	if in.PaymentMethod != nil {
		if !validPayments[*in.PaymentMethod] {
			return p, badEnum("unsupported paymentMethod %q", *in.PaymentMethod)
		}
		p.PaymentMethod = in.PaymentMethod
	}
	return p, nil
}
