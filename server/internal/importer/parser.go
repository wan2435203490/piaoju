package importer

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"piaoju/internal/platform/apperr"
)

// 账单来源（契约 §6.2 source 枚举）。
const (
	SourceWechat = "wechat"
	SourceAlipay = "alipay"
)

// cst 账单里的时间是本地时间（+08:00），统一按东八区解析后转 UTC（conventions §1）。
// 用固定时区而非 time.LoadLocation，避免容器缺 tzdata 时行为漂移。
var cst = time.FixedZone("CST", 8*60*60)

// maxNoteLen 对齐 migrations note VARCHAR(500)。
const maxNoteLen = 500

// 归一化后的列语义键。
const (
	colTime         = "time"         // 交易时间
	colAmount       = "amount"       // 金额（元）
	colDirection    = "direction"    // 收/支
	colCounterparty = "counterparty" // 交易对方
	colItem         = "item"         // 商品 / 商品说明
	colMethod       = "method"       // 支付方式 / 收付款方式
	colStatus       = "status"       // 当前状态 / 交易状态
	colType         = "type"         // 交易类型 / 交易分类（分类线索）
)

// columnAliases 表头名 → 语义键。**按表头名映射，不按列序号**（导出版本会加列/挪列）。
// 表头单元格先经 normHeader 归一（去空白/BOM、全角括号转半角），再查这张表。
var columnAliases = map[string]map[string]string{
	SourceWechat: {
		"交易时间":  colTime,
		"交易类型":  colType,
		"交易对方":  colCounterparty,
		"商品":    colItem,
		"商品名称":  colItem,
		"商品说明":  colItem,
		"收/支":   colDirection,
		"收支":    colDirection,
		"金额(元)": colAmount,
		"金额":    colAmount,
		"支付方式":  colMethod,
		"当前状态":  colStatus,
		"交易状态":  colStatus,
	},
	SourceAlipay: {
		"交易时间":   colTime,
		"交易创建时间": colTime,
		"付款时间":   colTime,
		"交易分类":   colType,
		"交易类型":   colType,
		"交易对方":   colCounterparty,
		"对方":     colCounterparty,
		"商品说明":   colItem,
		"商品名称":   colItem,
		"商品":     colItem,
		"收/支":    colDirection,
		"收支":     colDirection,
		"金额":     colAmount,
		"金额(元)":  colAmount,
		"收/付款方式": colMethod,
		"付款方式":   colMethod,
		"支付方式":   colMethod,
		"交易状态":   colStatus,
		"当前状态":   colStatus,
	},
}

// requiredCols 缺任一 → 40001（不是该来源的账单格式）。
var requiredCols = []string{colTime, colAmount, colDirection, colCounterparty}

// 收支值 → direction。「不计收支」的行（转账到自己账户、还款等）整行跳过。
const dirSkip = ""

// 交易状态里出现这些词 → 整行跳过（退款/关闭/失败，不该进账本）。
var skipStatusKeywords = []string{"退款", "交易关闭", "已关闭", "已取消", "失败", "冻结"}

// parsedRow 解析 + 归一化后的一行（occurredAt 保留 time.Time 供查重比对）。
type parsedRow struct {
	RowIndex      int
	AmountCents   int64
	Direction     string
	OccurredAt    time.Time
	Note          string
	PaymentMethod string
	CategoryID    int64
}

func badFormat(format string, a ...any) error {
	return apperr.New(apperr.CodeInvalidParam, fmt.Sprintf(format, a...))
}

// parse 账单 CSV → 归一化行。
//
// 流程：解码（UTF-8/BOM/GBK）→ 跳过前置说明行找到真正的表头 → 按表头名建列映射
// → 逐行归一化（金额转分、时间转 UTC、收支转 direction、支付方式转枚举）→ 规则分类。
// 「不计收支」与退款/关闭/失败的行直接跳过，不出现在结果里。
func parse(source string, data []byte) ([]parsedRow, error) {
	aliases, ok := columnAliases[source]
	if !ok {
		return nil, apperr.New(apperr.CodeUnsupportedEnum, fmt.Sprintf("unsupported source %q", source))
	}
	text, err := decode(data)
	if err != nil {
		return nil, err
	}

	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1 // 说明行/汇总行列数与表头不同，不能让 csv 报错
	r.LazyQuotes = true    // 账单里常见未转义的裸引号（如 商品名 22"显示器）
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, badFormat("csv parse failed: not a valid %s bill export", source)
	}

	cols, headerIdx := findHeader(records, aliases)
	if cols == nil {
		return nil, badFormat("missing required columns (%s): not a valid %s bill export",
			strings.Join(requiredCols, "/"), source)
	}

	rows := make([]parsedRow, 0, len(records))
	for i := headerIdx + 1; i < len(records); i++ {
		rec := records[i]
		row, ok, err := normalizeRow(source, cols, rec, i+1) // rowIndex 用 CSV 物理行号（1-based）
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// decode 账单字节 → UTF-8 文本。微信导出 UTF-8（可能带 BOM），支付宝常见 GBK。
// 先按 UTF-8 试；不是合法 UTF-8 → 按 GBK（GB18030 超集，兼容 GBK/GB2312）转码。
func decode(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	if utf8.Valid(data) {
		return string(data), nil
	}
	out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), data)
	if err != nil || !utf8.Valid(out) {
		return "", badFormat("unsupported file encoding (expect UTF-8 or GBK)")
	}
	return string(out), nil
}

// findHeader 扫描前置说明行，返回第一行「含全部必需列」的表头的列映射（语义键 → 列下标）与行号。
func findHeader(records [][]string, aliases map[string]string) (map[string]int, int) {
	for i, rec := range records {
		cols := make(map[string]int, len(rec))
		for j, cell := range rec {
			if key, ok := aliases[normHeader(cell)]; ok {
				if _, dup := cols[key]; !dup { // 同义列重复出现时以最左一列为准
					cols[key] = j
				}
			}
		}
		missing := false
		for _, need := range requiredCols {
			if _, ok := cols[need]; !ok {
				missing = true
				break
			}
		}
		if !missing {
			return cols, i
		}
	}
	return nil, -1
}

// normHeader 表头归一：去 BOM/空白（含全角空格）、全角括号转半角、去尾部冒号。
func normHeader(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	s = strings.NewReplacer(
		"（", "(", "）", ")",
		" ", "", "\t", "", "\r", "", "\n", "", "　", "",
	).Replace(s)
	return strings.TrimRight(s, ":：")
}

// cell 取列值；列不存在或该行列数不足 → ""（账单常有短行/汇总行）。
func cell(rec []string, cols map[string]int, key string) string {
	idx, ok := cols[key]
	if !ok || idx >= len(rec) {
		return ""
	}
	return cleanCell(rec[idx])
}

// cleanCell 去掉导出常见的前导制表符/空白与占位符引号。
func cleanCell(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "\uFEFF"))
	s = strings.Trim(s, "\t ")
	return strings.TrimSpace(s)
}

// normalizeRow 一行 CSV → parsedRow。ok=false 表示该行按规则跳过（空行/不计收支/退款/关闭）。
func normalizeRow(source string, cols map[string]int, rec []string, rowIndex int) (parsedRow, bool, error) {
	var row parsedRow

	rawTime := cell(rec, cols, colTime)
	rawAmount := cell(rec, cols, colAmount)
	rawDir := cell(rec, cols, colDirection)

	// 空行 / 汇总行（「共 12 笔记录」之类）：关键字段全空 → 跳过，不算格式错误。
	if rawTime == "" && rawAmount == "" && rawDir == "" {
		return row, false, nil
	}

	dir := parseDirection(rawDir)
	if dir == dirSkip { // 不计收支（转账到自己账户、还款等）
		return row, false, nil
	}
	if skipStatus(cell(rec, cols, colStatus)) { // 退款/交易关闭/失败
		return row, false, nil
	}

	occurredAt, err := parseTime(rawTime)
	if err != nil {
		return row, false, badFormat("row %d: %v", rowIndex, err)
	}
	cents, err := parseAmountCents(rawAmount)
	if err != nil {
		return row, false, badFormat("row %d: %v", rowIndex, err)
	}

	counterparty := placeholder(cell(rec, cols, colCounterparty))
	item := placeholder(cell(rec, cols, colItem))
	kind := placeholder(cell(rec, cols, colType))

	row = parsedRow{
		RowIndex:      rowIndex,
		AmountCents:   cents,
		Direction:     dir,
		OccurredAt:    occurredAt,
		Note:          buildNote(counterparty, item),
		PaymentMethod: parsePaymentMethod(source, cell(rec, cols, colMethod)),
		CategoryID:    classify(dir, counterparty, item, kind),
	}
	return row, true, nil
}

// placeholder 账单用 "/" 表示空值。
func placeholder(s string) string {
	if s == "/" || s == "-" {
		return ""
	}
	return s
}

// buildNote note = 交易对方 + 商品（去重、去空），截断到列宽。
func buildNote(counterparty, item string) string {
	var parts []string
	if counterparty != "" {
		parts = append(parts, counterparty)
	}
	if item != "" && item != counterparty {
		parts = append(parts, item)
	}
	note := strings.Join(parts, " ")
	if utf8.RuneCountInString(note) > maxNoteLen {
		note = string([]rune(note)[:maxNoteLen])
	}
	return note
}

// parseDirection 收/支 → direction；「不计收支」及无法识别的值 → dirSkip（整行跳过）。
func parseDirection(s string) string {
	switch {
	case strings.Contains(s, "支出"):
		return "expense"
	case strings.Contains(s, "收入"):
		return "income"
	default: // 不计收支 / "/" / 空
		return dirSkip
	}
}

func skipStatus(status string) bool {
	for _, kw := range skipStatusKeywords {
		if strings.Contains(status, kw) {
			return true
		}
	}
	return false
}

// timeLayouts 账单里出现过的时间格式（一律本地 +08:00）。
var timeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006/01/02 15:04:05",
	"2006-01-02 15:04",
	"2006/01/02 15:04",
	"2006-01-02T15:04:05",
}

// parseTime 账单本地时间（+08:00）→ UTC，截断到毫秒（DATETIME(3) 精度）。
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, cst); err == nil {
			return t.UTC().Truncate(time.Millisecond), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", s)
}

var errBadAmount = errors.New("invalid amount")

// parseAmountCents 「元」字符串 → 整数分。吃 "¥12.30" / "12.30" / "1,234.50" / "12.3" / "12"。
// 不用 float：金额必须精确（conventions §1）。
func parseAmountCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	// 去掉货币符号、千分位、空白、方向前缀（部分导出写 "-12.30" / "+12.30"）。
	s = strings.NewReplacer("¥", "", "￥", "", "$", "", ",", "", "，", "", " ", "", " ", "").Replace(s)
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "-") // 方向由「收/支」列决定，金额一律取绝对值
	if s == "" {
		return 0, errBadAmount
	}

	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	yuan, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w %q", errBadAmount, s)
	}
	switch len(fracPart) {
	case 0:
		fracPart = "00"
	case 1:
		fracPart += "0"
	case 2:
	default:
		fracPart = fracPart[:2] // 多余精度截断（账单不会出现，防御）
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w %q", errBadAmount, s)
	}
	return yuan*100 + frac, nil
}

// paymentKeywords 支付方式列内容 → 契约 §4 paymentMethod 枚举（顺序敏感：先具体后宽泛）。
var paymentKeywords = []struct {
	keys   []string
	method string
}{
	{[]string{"信用卡", "储蓄卡", "借记卡", "银行卡", "银行"}, "card"},
	{[]string{"零钱", "微信"}, "wechat"},
	{[]string{"花呗", "余额宝", "余额", "支付宝", "网商"}, "alipay"},
	{[]string{"现金"}, "cash"},
}

// parsePaymentMethod 支付方式列 → 枚举。空值（"/"）按来源默认（微信→wechat，支付宝→alipay）；
// 有值但认不出 → other。
func parsePaymentMethod(source, s string) string {
	s = placeholder(s)
	if s == "" {
		switch source {
		case SourceWechat:
			return "wechat"
		case SourceAlipay:
			return "alipay"
		}
		return "other"
	}
	for _, m := range paymentKeywords {
		for _, k := range m.keys {
			if strings.Contains(s, k) {
				return m.method
			}
		}
	}
	return "other"
}

// readAllLimit 读满 limit+1 字节即返回，交由调用方判超限。
func readAllLimit(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit+1))
}
