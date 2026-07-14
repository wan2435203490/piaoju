package importer

// importer 单测：解析 → 归一化 → 规则分类 → 查重全链路。
// 本机无 Docker/MySQL（同 transaction 模块约束），查重那步用 go-sqlmock 精确断言
// SQL + 参数（regexp.QuoteMeta 复用 service.go 的 SQL 常量，防实现/断言漂移）。
//
// user_id 隔离：查重 SQL 常量本身带 user_id = ?，且所有 WithArgs 均含 uid。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/token"
)

const uidA int64 = 7

// wechatCSV 微信支付导出样本：前置说明行 + 表头 + 正常行 / 千分位 / 不计收支 / 退款 / 收入。
const wechatCSV = `微信支付账单明细
微信昵称：[测试用户]
起始时间：[2026-07-01 00:00:00] 终止时间：[2026-07-13 23:59:59]
导出类型：[全部]
共5笔记录
----------------------微信支付账单明细列表--------------------
交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注
2026-07-10 12:30:45,商户消费,瑞幸咖啡,生椰拿铁,支出,¥19.90,零钱,支付成功,42001,10001,/
2026-07-11 08:05:00,商户消费,滴滴出行,滴滴快车,支出,"¥1,234.50",招商银行储蓄卡(1234),支付成功,42002,10002,/
2026-07-11 19:00:00,转账,零钱通,零钱通-转入,不计收支,¥500.00,零钱,已转入零钱通,42003,10003,/
2026-07-12 10:00:00,商户消费,某某服饰旗舰店,纯棉T恤,支出,¥88.00,零钱,已全额退款,42004,10004,/
2026-07-12 20:00:00,微信红包,李四,微信红包-来自李四,收入,¥66.00,零钱,已存入零钱,42005,10005,/
`

// alipayCSV 支付宝导出样本（新版列名：交易分类 / 商品说明 / 收/付款方式 / 交易状态）。
const alipayCSV = `------------------------支付宝交易记录明细查询------------------------
账号:[test@example.com]
起始日期:[2026-07-01 00:00:00]    终止日期:[2026-07-13 23:59:59]
---------------------------------交易记录明细列表---------------------------------
交易时间,交易分类,交易对方,对方账号,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号,备注
2026-07-05 09:12:00,交通出行,滴滴出行,didi@example.com,滴滴快车,支出,25.00,余额宝,交易成功,T1,M1,
2026-07-06 12:00:00,餐饮美食,美团平台商户,mt@example.com,外卖订单,支出,"1,088.00",花呗,交易成功,T2,M2,
2026-07-07 15:00:00,退款,天猫超市,tm@example.com,退款-抽纸,收入,9.90,余额,退款成功,T3,M3,
2026-07-08 10:00:00,转账,余额宝,/,余额宝-单次转入,不计收支,1000.00,余额,交易成功,T4,M4,
2026-07-09 18:00:00,收入,某某科技有限公司,/,工资发放,收入,12000,招商银行储蓄卡(6789),交易成功,T5,M5,
共 5 笔记录
------------------------支付宝(中国)网络技术有限公司  电子对账单------------------------
`

func newTestService(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &service{db: db}, mock
}

func mustMeet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func wantCode(t *testing.T, err error, code int) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != code {
		t.Fatalf("err = %v, want apperr code %d", err, code)
	}
}

// dupCols 与 sqlSelectDupCandidates 列序一一对应。
func dupCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"amount_cents", "occurred_at"})
}

// expectNoDups 期望一次批量查重 query（不是每行一个 —— 期望只声明一次，多查即失败），返回空候选集。
func expectNoDups(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectDupCandidates)).
		WithArgs(uidA, sqlmock.AnyArg(), sqlmock.AnyArg(), maxDupCandidates).
		WillReturnRows(dupCols())
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// ── 全链路：微信 ──────────────────────────────────────────────────────────────

func TestPreviewWechat(t *testing.T) {
	svc, mock := newTestService(t)
	expectNoDups(mock)

	res, err := svc.preview(context.Background(), uidA, SourceWechat, []byte(wechatCSV))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	mustMeet(t, mock)

	// 5 行数据 → 不计收支（零钱通转入）与退款（已全额退款）各跳过 1 行 → 3 行。
	if res.Total != 3 || len(res.Items) != 3 {
		t.Fatalf("total = %d, items = %d, want 3/3: %+v", res.Total, len(res.Items), res.Items)
	}
	if res.Duplicates != 0 {
		t.Fatalf("duplicates = %d, want 0", res.Duplicates)
	}

	want := []ImportRow{
		{RowIndex: 8, AmountCents: 1990, Direction: "expense", OccurredAt: "2026-07-10T04:30:45Z",
			Note: "瑞幸咖啡 生椰拿铁", PaymentMethod: "wechat", CategoryID: catDrink},
		{RowIndex: 9, AmountCents: 123450, Direction: "expense", OccurredAt: "2026-07-11T00:05:00Z",
			Note: "滴滴出行 滴滴快车", PaymentMethod: "card", CategoryID: catTransport},
		{RowIndex: 12, AmountCents: 6600, Direction: "income", OccurredAt: "2026-07-12T12:00:00Z",
			Note: "李四 微信红包-来自李四", PaymentMethod: "wechat", CategoryID: catRedPacket},
	}
	for i, w := range want {
		if res.Items[i] != w {
			t.Errorf("item[%d] = %+v, want %+v", i, res.Items[i], w)
		}
	}
}

// ── 全链路：支付宝（含 GBK 编码）────────────────────────────────────────────────

func TestPreviewAlipayGBK(t *testing.T) {
	gbk, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(alipayCSV))
	if err != nil {
		t.Fatalf("encode gbk: %v", err)
	}
	if utf8.Valid(gbk) {
		t.Fatal("sample must not be valid utf-8, otherwise the GBK path is not exercised")
	}

	svc, mock := newTestService(t)
	expectNoDups(mock)

	res, err := svc.preview(context.Background(), uidA, SourceAlipay, gbk)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	mustMeet(t, mock)

	// 5 行数据 → 退款成功 + 不计收支 各跳 1 行 → 3 行（汇总行/尾注不算行）。
	want := []ImportRow{
		{RowIndex: 6, AmountCents: 2500, Direction: "expense", OccurredAt: "2026-07-05T01:12:00Z",
			Note: "滴滴出行 滴滴快车", PaymentMethod: "alipay", CategoryID: catTransport},
		{RowIndex: 7, AmountCents: 108800, Direction: "expense", OccurredAt: "2026-07-06T04:00:00Z",
			Note: "美团平台商户 外卖订单", PaymentMethod: "alipay", CategoryID: catFood},
		{RowIndex: 10, AmountCents: 1200000, Direction: "income", OccurredAt: "2026-07-09T10:00:00Z",
			Note: "某某科技有限公司 工资发放", PaymentMethod: "card", CategoryID: catSalary},
	}
	if res.Total != len(want) {
		t.Fatalf("total = %d, want %d: %+v", res.Total, len(want), res.Items)
	}
	for i, w := range want {
		if res.Items[i] != w {
			t.Errorf("item[%d] = %+v, want %+v", i, res.Items[i], w)
		}
	}
}

// ── 查重（批量单 query，必带 user_id）────────────────────────────────────────────

func TestPreviewMarksDuplicates(t *testing.T) {
	svc, mock := newTestService(t)

	// 契约 §6.2：duplicate = 同金额 + 同时刻（精确相等，不看方向、不设时间窗）。
	// 账单最早 2026-07-10T04:30:45Z、最晚 2026-07-12T12:00:00Z → 查询区间即 [lo, hi]（无外扩）。
	lo := utc("2026-07-10T04:30:45Z")
	hi := utc("2026-07-12T12:00:00Z")

	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectDupCandidates)).
		WithArgs(uidA, lo, hi, maxDupCandidates).
		WillReturnRows(dupCols().
			// 同金额 + 同时刻 → 命中瑞幸那笔（expense 1990 @04:30:45）
			AddRow(1990, utc("2026-07-10T04:30:45Z")).
			// 同金额但相差 30s（≠ 同时刻）→ 不算重复：证明时间窗已移除（滴滴那笔 @00:05:00）
			AddRow(123450, utc("2026-07-11T00:05:30Z")).
			// 同金额 + 同时刻，库中方向不同也算重复 → 命中红包那笔（bill income 6600 @12:00:00）
			AddRow(6600, utc("2026-07-12T12:00:00Z")))

	res, err := svc.preview(context.Background(), uidA, SourceWechat, []byte(wechatCSV))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	mustMeet(t, mock)

	if res.Duplicates != 2 {
		t.Fatalf("duplicates = %d, want 2", res.Duplicates)
	}
	got := []bool{res.Items[0].Duplicate, res.Items[1].Duplicate, res.Items[2].Duplicate}
	if want := []bool{true, false, true}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("duplicate flags = %v, want %v", got, want)
	}
}

// TestPreviewEmptyBillSkipsQuery 无可导入行时不该发查重 query（mock 未声明任何 query → 多查即失败）。
func TestPreviewEmptyBillSkipsQuery(t *testing.T) {
	svc, mock := newTestService(t)

	csv := "微信支付账单明细\n交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态\n" +
		"2026-07-10 12:30:45,转账,自己,零钱通转入,不计收支,¥500.00,零钱,已转入,\n"
	res, err := svc.preview(context.Background(), uidA, SourceWechat, []byte(csv))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	mustMeet(t, mock)

	if res.Total != 0 || len(res.Items) != 0 {
		t.Fatalf("total = %d, want 0", res.Total)
	}
}

// ── 编码 / 表头 / 格式错误 ─────────────────────────────────────────────────────

func TestPreviewUTF8BOM(t *testing.T) {
	svc, mock := newTestService(t)
	expectNoDups(mock)

	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(wechatCSV)...)
	res, err := svc.preview(context.Background(), uidA, SourceWechat, data)
	if err != nil {
		t.Fatalf("preview with BOM: %v", err)
	}
	mustMeet(t, mock)
	if res.Total != 3 {
		t.Fatalf("total = %d, want 3", res.Total)
	}
}

func TestParseMissingRequiredColumn(t *testing.T) {
	// 去掉「金额(元)」列 → 关键列缺失 → 40001（不是该来源的账单格式）。
	csv := "微信支付账单明细\n交易时间,交易类型,交易对方,商品,收/支,支付方式,当前状态\n" +
		"2026-07-10 12:30:45,商户消费,瑞幸咖啡,拿铁,支出,零钱,支付成功\n"
	_, err := parse(SourceWechat, []byte(csv))
	wantCode(t, err, apperr.CodeInvalidParam)
}

func TestParseNotABill(t *testing.T) {
	_, err := parse(SourceAlipay, []byte("hello,world\n1,2\n"))
	wantCode(t, err, apperr.CodeInvalidParam)
}

func TestParseUnsupportedSource(t *testing.T) {
	_, err := parse("unionpay", []byte(wechatCSV))
	wantCode(t, err, apperr.CodeUnsupportedEnum)
}

// TestParseRowCap 恶意/畸形文件（超行数上限）→ 40001，且不会把整份读进内存（DoS 防御）。
// 用 maxRows+2 条纯逗号行触发行数上限；解析在越限时立即中止。
func TestParseRowCap(t *testing.T) {
	csv := strings.Repeat(",\n", maxRows+2)
	_, err := parse(SourceWechat, []byte(csv))
	wantCode(t, err, apperr.CodeInvalidParam)
}

// TestParseColCap 单行列数超上限（如 5MB 全逗号挤在一行）→ 40001，不建百万字段行。
func TestParseColCap(t *testing.T) {
	csv := strings.Repeat(",", maxCols+1) + "\n"
	_, err := parse(SourceWechat, []byte(csv))
	wantCode(t, err, apperr.CodeInvalidParam)
}

// TestParseRowCapAllowsLegitBill maxRows 之内的正常账单不受影响（不误伤）。
func TestParseRowCapAllowsLegitBill(t *testing.T) {
	if _, err := parse(SourceWechat, []byte(wechatCSV)); err != nil {
		t.Fatalf("legit bill within caps must parse: %v", err)
	}
}

// TestParseHeaderAliases 表头按名映射：列顺序变化 / 旧版列名（金额（元）全角括号、交易状态）照吃。
func TestParseHeaderAliases(t *testing.T) {
	csv := "支付宝交易记录明细查询\n" +
		"收/支,金额（元）,交易时间,交易对方,商品说明,付款方式,交易状态\n" +
		"支出,¥ 12.30,2026/07/10 08:00:00,星巴克,拿铁,余额,交易成功\n"
	rows, err := parse(SourceAlipay, []byte(csv))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.AmountCents != 1230 || r.PaymentMethod != "alipay" || r.CategoryID != catDrink {
		t.Fatalf("row = %+v", r)
	}
	if got := r.OccurredAt.Format(time.RFC3339); got != "2026-07-10T00:00:00Z" {
		t.Fatalf("occurredAt = %s, want 2026-07-10T00:00:00Z", got)
	}
}

// ── 归一化单元 ───────────────────────────────────────────────────────────────

func TestParseAmountCents(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"12.30", 1230},
		{"¥12.30", 1230},
		{"￥12.3", 1230},
		{"1,234.50", 123450},
		{"12,345,678.09", 1234567809},
		{"12000", 1200000},
		{"0.05", 5},
		{"-12.30", 1230}, // 方向由「收/支」列决定，金额取绝对值
		{"0.00", 0},      // 0 元行：允许导入成 amountCents=0
		{"0", 0},
		{"10000000000.00", maxAmountCents}, // 上界（100 亿元）恰好放行
	}
	for _, c := range cases {
		got, err := parseAmountCents(c.in)
		if err != nil || got != c.want {
			t.Errorf("parseAmountCents(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	// 溢出/越界必须报错，不能静默回绕成负额（否则绕过 parser 到 sync/push 才被拒）。
	// "92233720368547759" 是合法 int64，但 *100 会溢出成负 amountCents。
	for _, bad := range []string{"", "abc", "¥", "/", "99999999999999999999", "92233720368547759.00"} {
		if _, err := parseAmountCents(bad); err == nil {
			t.Errorf("parseAmountCents(%q): want error", bad)
		}
	}
}

func TestParsePaymentMethod(t *testing.T) {
	cases := []struct {
		source, in, want string
	}{
		{SourceWechat, "零钱", "wechat"},
		{SourceWechat, "招商银行储蓄卡(1234)", "card"},
		{SourceWechat, "/", "wechat"}, // 空值 → 来源默认
		{SourceAlipay, "", "alipay"},  // 空值 → 来源默认
		{SourceAlipay, "余额宝", "alipay"},
		{SourceAlipay, "花呗", "alipay"},
		{SourceAlipay, "现金", "cash"},
		{SourceAlipay, "数字人民币", "other"}, // 认不出 → other
	}
	for _, c := range cases {
		if got := parsePaymentMethod(c.source, c.in); got != c.want {
			t.Errorf("parsePaymentMethod(%s, %q) = %s, want %s", c.source, c.in, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		dir, counterparty, item, kind string
		want                          int64
	}{
		{"expense", "美团平台商户", "外卖订单", "", catFood},
		{"expense", "饿了么", "晚餐", "", catFood},
		{"expense", "蜜雪冰城", "柠檬水", "", catDrink},
		{"expense", "星巴克", "拿铁", "", catDrink},
		{"expense", "滴滴出行", "快车", "", catTransport},
		{"expense", "中国铁路12306", "车票", "", catTransport},
		{"expense", "中国石化", "加油", "", catTransport},
		{"expense", "淘宝", "T恤", "", catShopping},
		{"expense", "拼多多", "手机壳", "", catShopping},
		{"expense", "猫眼电影", "电影票", "", catFun},
		{"expense", "Steam", "游戏", "", catFun},
		{"expense", "全家便利店", "矿泉水", "", catDaily},
		{"expense", "老百姓大药房", "感冒灵", "", catMedical},
		{"expense", "某某商户", "不明消费", "", catOther},
		{"income", "某某科技有限公司", "工资发放", "收入", catSalary},
		{"income", "李四", "微信红包-来自李四", "", catRedPacket},
		{"income", "某某平台", "补贴", "", catOtherIn},
	}
	for _, c := range cases {
		if got := classify(c.dir, c.counterparty, c.item, c.kind); got != c.want {
			t.Errorf("classify(%s, %q, %q) = %d, want %d", c.dir, c.counterparty, c.item, got, c.want)
		}
	}
}

// ── HTTP 层（契约 §6.2）──────────────────────────────────────────────────────

func newTestServer(t *testing.T) (*httptest.Server, sqlmock.Sqlmock, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tm := token.NewManager("importer-test-secret")
	access, err := tm.Sign(uidA, time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))
			sec.Mount("/imports", Routes(db)) // 与主线程挂载方式一致
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, mock, access
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// postPreview 发一次 multipart 预览请求。
func postPreview(t *testing.T, srv *httptest.Server, access, source string, csv []byte) envelope {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if source != "" {
		if err := mw.WriteField("source", source); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	fw, err := mw.CreateFormFile("file", "bill.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(csv); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/imports/preview", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+access)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}

func TestHandlerPreviewOK(t *testing.T) {
	srv, mock, access := newTestServer(t)
	expectNoDups(mock)

	env := postPreview(t, srv, access, SourceWechat, []byte(wechatCSV))
	if env.Code != apperr.CodeOK {
		t.Fatalf("code = %d (%s), want 0", env.Code, env.Message)
	}
	mustMeet(t, mock)

	var res previewResult
	if err := json.Unmarshal(env.Data, &res); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if res.Total != 3 || res.Duplicates != 0 || len(res.Items) != 3 {
		t.Fatalf("data = %+v", res)
	}
	if res.Items[0].OccurredAt != "2026-07-10T04:30:45Z" {
		t.Fatalf("occurredAt = %s", res.Items[0].OccurredAt)
	}
}

func TestHandlerMissingSource(t *testing.T) {
	srv, _, access := newTestServer(t)
	if env := postPreview(t, srv, access, "", []byte(wechatCSV)); env.Code != apperr.CodeInvalidParam {
		t.Fatalf("code = %d, want %d", env.Code, apperr.CodeInvalidParam)
	}
}

func TestHandlerBadSource(t *testing.T) {
	srv, _, access := newTestServer(t)
	if env := postPreview(t, srv, access, "unionpay", []byte(wechatCSV)); env.Code != apperr.CodeUnsupportedEnum {
		t.Fatalf("code = %d, want %d", env.Code, apperr.CodeUnsupportedEnum)
	}
}

func TestHandlerFileTooLarge(t *testing.T) {
	srv, _, access := newTestServer(t)

	// >5MB：表头 + 大量填充行（契约 §6.2 → 41301）。
	big := []byte("交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态\n" +
		strings.Repeat("2026-07-10 12:30:45,商户消费,瑞幸咖啡,拿铁,支出,¥19.90,零钱,支付成功\n", 120000))
	if int64(len(big)) <= maxBytes {
		t.Fatalf("fixture is %d bytes, must exceed %d", len(big), maxBytes)
	}
	if env := postPreview(t, srv, access, SourceWechat, big); env.Code != apperr.CodeUploadTooLarge {
		t.Fatalf("code = %d, want %d", env.Code, apperr.CodeUploadTooLarge)
	}
}

func TestHandlerUnauthorized(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if env := postPreview(t, srv, "", SourceWechat, []byte(wechatCSV)); env.Code != apperr.CodeTokenExpired {
		t.Fatalf("code = %d, want %d", env.Code, apperr.CodeTokenExpired)
	}
}
