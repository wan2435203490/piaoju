// Package sync 契约 §8：离线同步（push / pull）。
//
// 跨模块访问豁免声明（conventions §3「模块间不许互查对方的表」的显式豁免）：
// 本模块按契约 §8 需要在单个 DB 事务内同时读写 transactions / tickets / attachments /
// categories 四张表——push 的票↔交易联动一致性（建票同事务建交易、删票同事务删交易）
// 与 pull 的多实体统一游标下发，都无法通过其他模块的 service 接口表达。豁免范围仅限：
//   - transactions：读 + 幂等 upsert + 软删墓碑（direction 建后不可改，与 transaction 模块同规则）
//   - tickets：读 + 幂等 upsert + 软删墓碑（建票必带交易，与 ticket 模块 create 语义一致）
//   - attachments：仅 ticket_id 绑定/解绑（不写文件路径等 upload 私有字段）
//   - categories：只读（pull 下发）
//
// 不得在本模块内扩大到其他表或其他写语义；任何新增写路径需先改契约。
package sync

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/upload"
)

// 枚举（契约 §8 / §4 / §5）。
var (
	validEntities  = map[string]bool{"transaction": true, "ticket": true}
	validOps       = map[string]bool{"upsert": true, "delete": true}
	validDirection = map[string]bool{"expense": true, "income": true}
	validPayments  = map[string]bool{
		"wechat": true, "alipay": true, "cash": true, "card": true, "other": true,
	}
	validKinds = map[string]bool{
		"movie": true, "show": true, "attraction": true,
		"train": true, "flight": true, "other": true,
	}
)

// extraWhitelist 契约 §5「extra 按 kind」白名单（唯一事实来源为 docs/PROTOCOL.md §5 表格）。
// 与 ticket 模块同表，但 ticket 的版本不可导出——契约变更时两处必须同步改。
var extraWhitelist = map[string]map[string]bool{
	"movie":      set("cinema", "hall", "filmFormat"),
	"show":       set("tour", "session", "zone"),
	"attraction": set("city", "ticketType"),
	"train":      set("trainNo", "fromStation", "toStation", "departTime", "arriveTime", "seatClass"),
	"flight":     set("flightNo", "airline", "fromAirport", "toAirport", "departTime", "arriveTime", "cabin"),
	"other":      set(),
}

func set(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// 字段上限（对齐 migrations/0003 列宽与 transaction/ticket 模块的校验，防两条写路径宽严不一）。
const (
	maxNoteLen       = 500
	maxTitleLen      = 128
	maxVenueLen      = 128
	maxSeatLen       = 64
	maxMemoLen       = 5000
	maxExtraValueLen = 512
	maxAttachments   = 20
	maxRating        = 5

	// maxChanges 单次 push 的 change 上限；超出 → 整请求 40001（防超大事务串行阻塞）。
	maxChanges = 500
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ── push 请求/响应（契约 §8）────────────────────────────────────────────────

// pushBody POST /sync/push 请求体。
type pushBody struct {
	Changes []rawChange `json:"changes"`
}

// rawChange 单条变更；payload 延迟解码（entity/op 决定形状），
// 且单条解码失败只影响本条结果（status=error），不拖垮整批。
type rawChange struct {
	Entity          string          `json:"entity"`
	Op              string          `json:"op"`
	Payload         json.RawMessage `json:"payload"`
	ClientUpdatedAt string          `json:"clientUpdatedAt"`
}

// pushResult 契约 §8 data。
type pushResult struct {
	Results []changeResult `json:"results"`
}

// changeResult 单条变更结果。
// status: applied（已写入）| stale（客户端版本更旧，服务端版本随 pull 下发）| error。
// code: applied=0；stale=40902；error=对应 apperr 码。
type changeResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Code   int    `json:"code"`
}

const (
	statusApplied = "applied"
	statusStale   = "stale"
	statusError   = "error"
)

// change 校验通过的单条变更。
type change struct {
	Entity        string
	Op            string
	ID            string
	ClientUpdated time.Time
	Tx            *txData     // entity=transaction && op=upsert
	Ticket        *ticketData // entity=ticket && op=upsert
}

// txData TransactionInput（契约 §4）。
type txData struct {
	ID            string
	AmountCents   int64
	Direction     string
	CategoryID    int64
	Note          string
	OccurredAt    time.Time
	PaymentMethod string
}

// ticketData TicketInput（契约 §5）。AttachmentIDs 为 nil 表示未提供（不动附件绑定）。
// TransactionID 为客户端生成的联动交易主键（契约 §5 v1.2：离线建票时客户端必须知道交易 id）；
// 幂等重放时以库中已存在的 transaction_id 为准，不被 payload 覆盖（防交易分裂）。
type ticketData struct {
	ID            string
	TransactionID string
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
	AttachmentIDs *[]int64
}

// ── pull 响应实体（契约 §4 / §5 / §3 形状 + 墓碑 deletedAt）─────────────────

// pullResult 契约 §8 data。nextCursor 为服务端单调游标（不透明串）；
// 无新数据时回显入参 since（客户端水位不回退）。
type pullResult struct {
	Transactions []*Transaction `json:"transactions"`
	Tickets      []*Ticket      `json:"tickets"`
	Categories   []*Category    `json:"categories"`
	NextCursor   string         `json:"nextCursor"`
	HasMore      bool           `json:"hasMore"`
}

// Transaction 契约 §4 + deletedAt（墓碑行非空，客户端据此删本地）。
type Transaction struct {
	ID            string  `json:"id"`
	AmountCents   int64   `json:"amountCents"`
	Direction     string  `json:"direction"`
	CategoryID    int64   `json:"categoryId"`
	Note          string  `json:"note"`
	OccurredAt    string  `json:"occurredAt"`
	PaymentMethod string  `json:"paymentMethod"`
	TicketID      *string `json:"ticketId"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
	DeletedAt     *string `json:"deletedAt"`
}

// Ticket 契约 §5 + deletedAt。
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
	DeletedAt   *string             `json:"deletedAt"`
}

// TxSummary 契约 §5 内嵌只读交易摘要。
type TxSummary struct {
	ID            string `json:"id"`
	AmountCents   int64  `json:"amountCents"`
	CategoryID    int64  `json:"categoryId"`
	PaymentMethod string `json:"paymentMethod"`
}

// Category 契约 §3 + deletedAt。
type Category struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Icon      string  `json:"icon"`
	Kind      string  `json:"kind"`
	IsSystem  bool    `json:"isSystem"`
	Sort      int     `json:"sort"`
	DeletedAt *string `json:"deletedAt"`
}

// ── 校验 ────────────────────────────────────────────────────────────────────

func badParam(format string, a ...any) error {
	return apperr.New(apperr.CodeInvalidParam, fmt.Sprintf(format, a...))
}

func badEnum(format string, a ...any) error {
	return apperr.New(apperr.CodeUnsupportedEnum, fmt.Sprintf(format, a...))
}

// parseRFC3339 契约时间格式；统一转 UTC、截断到毫秒（DATETIME(3) 精度）。
func parseRFC3339(s, field string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, badParam("%s must be RFC3339 (e.g. 2026-07-12T11:30:00Z)", field)
	}
	return t.UTC().Truncate(time.Millisecond), nil
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func rfc3339Ptr(t time.Time, valid bool) *string {
	if !valid {
		return nil
	}
	s := rfc3339(t)
	return &s
}

// idOnly delete 载荷 { id }，以及解析失败时的 best-effort 取 id（结果需回带 id）。
type idOnly struct {
	ID *string `json:"id"`
}

// bestEffortID 从任意 payload 里挖 id，供解析失败的 error 结果回带（挖不到则空串）。
func bestEffortID(payload json.RawMessage) string {
	var in idOnly
	if err := json.Unmarshal(payload, &in); err != nil || in.ID == nil {
		return ""
	}
	return normalizeID(*in.ID)
}

func normalizeID(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// parseChange 校验单条变更（entity/op/clientUpdatedAt/payload）。
func parseChange(raw rawChange) (change, error) {
	var c change

	if !validEntities[raw.Entity] {
		return c, badEnum("unsupported entity %q", raw.Entity)
	}
	if !validOps[raw.Op] {
		return c, badEnum("unsupported op %q", raw.Op)
	}
	c.Entity, c.Op = raw.Entity, raw.Op

	if raw.ClientUpdatedAt == "" {
		return c, badParam("clientUpdatedAt is required")
	}
	cu, err := parseRFC3339(raw.ClientUpdatedAt, "clientUpdatedAt")
	if err != nil {
		return c, err
	}
	c.ClientUpdated = cu

	if len(raw.Payload) == 0 {
		return c, badParam("payload is required")
	}

	if raw.Op == "delete" {
		var in idOnly
		if err := json.Unmarshal(raw.Payload, &in); err != nil || in.ID == nil {
			return c, badParam("delete payload must be { id }")
		}
		id := normalizeID(*in.ID)
		if !uuidRe.MatchString(id) {
			return c, badParam("id must be a UUID")
		}
		c.ID = id
		return c, nil
	}

	if raw.Entity == "transaction" {
		d, err := parseTxPayload(raw.Payload)
		if err != nil {
			return c, err
		}
		c.ID, c.Tx = d.ID, &d
		return c, nil
	}
	d, err := parseTicketPayload(raw.Payload)
	if err != nil {
		return c, err
	}
	c.ID, c.Ticket = d.ID, &d
	return c, nil
}

// txBody TransactionInput 解码结构（指针区分「未提供」与零值）。
type txBody struct {
	ID            *string `json:"id"`
	AmountCents   *int64  `json:"amountCents"`
	Direction     *string `json:"direction"`
	CategoryID    *int64  `json:"categoryId"`
	Note          *string `json:"note"`
	OccurredAt    *string `json:"occurredAt"`
	PaymentMethod *string `json:"paymentMethod"`
}

// parseTxPayload 校验 TransactionInput；缺省 note=""、paymentMethod="other"（与 transaction 模块同规则）。
func parseTxPayload(raw json.RawMessage) (txData, error) {
	var in txBody
	var d txData
	if err := json.Unmarshal(raw, &in); err != nil {
		return d, badParam("invalid transaction payload")
	}

	if in.ID == nil {
		return d, badParam("id is required (client-generated UUID)")
	}
	d.ID = normalizeID(*in.ID)
	if !uuidRe.MatchString(d.ID) {
		return d, badParam("id must be a UUID")
	}
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
	if !validDirection[*in.Direction] {
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
	return d, nil
}

// ticketBody TicketInput 解码结构。
type ticketBody struct {
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
}

// parseTicketPayload 校验 TicketInput（与 ticket 模块 parseCreate 同规则）。
func parseTicketPayload(raw json.RawMessage) (ticketData, error) {
	var in ticketBody
	var d ticketData
	if err := json.Unmarshal(raw, &in); err != nil {
		return d, badParam("invalid ticket payload")
	}

	if in.ID == nil {
		return d, badParam("id is required (client-generated UUID)")
	}
	d.ID = normalizeID(*in.ID)
	if !uuidRe.MatchString(d.ID) {
		return d, badParam("id must be a UUID")
	}

	// 契约 §5 v1.2：联动交易主键由客户端生成（必填）。
	if in.TransactionID == nil {
		return d, badParam("transactionId is required (client-generated UUID)")
	}
	d.TransactionID = normalizeID(*in.TransactionID)
	if !uuidRe.MatchString(d.TransactionID) {
		return d, badParam("transactionId must be a UUID")
	}
	if d.TransactionID == d.ID {
		return d, badParam("transactionId must differ from ticket id")
	}

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
		d.AttachmentIDs = &ids
	}
	return d, nil
}

// normalizeExtra 按 kind 白名单校验并归一化为完整形状（缺省空串）。
func normalizeExtra(kind string, raw map[string]any) (map[string]string, error) {
	allowed := extraWhitelist[kind]
	out := make(map[string]string, len(allowed))
	for k := range allowed {
		out[k] = ""
	}
	for k, v := range raw {
		if !allowed[k] {
			return nil, badParam("extra: unknown field %q for kind %q", k, kind)
		}
		s, ok := v.(string)
		if !ok {
			return nil, badParam("extra: field %q must be a string", k)
		}
		if utf8.RuneCountInString(s) > maxExtraValueLen {
			return nil, badParam("extra: field %q too long (max %d chars)", k, maxExtraValueLen)
		}
		out[k] = s
	}
	return out, nil
}

// fillExtraDefaults 读路径兜底：响应 extra 恒为该 kind 的完整形状。
func fillExtraDefaults(kind string, m map[string]string) map[string]string {
	if m == nil {
		m = make(map[string]string, len(extraWhitelist[kind]))
	}
	for k := range extraWhitelist[kind] {
		if _, ok := m[k]; !ok {
			m[k] = ""
		}
	}
	return m
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
