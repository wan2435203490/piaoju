package ticket

import "unicode/utf8"

// extraWhitelist 契约 §5「extra 按 kind」白名单，全部字段可空字符串。
// 唯一事实来源为 docs/PROTOCOL.md §5 表格；此处不得私自增删字段。
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

// normalizeExtra 校验 extra 并归一化为「白名单全字段、缺省空串」的完整形状（写入与响应共用）。
// 未知字段 / 非字符串值 / 过长 → 40001（任务卡：extra 校验白名单，未知字段 40001）。
// kind 必须已通过枚举校验（非法 kind 走 40002，在调用方处理）。
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

// reconcileExtra PATCH 改 kind 但未提供新 extra 时的兜底：
// 丢弃旧 extra 的空值字段后按新 kind 重校验；仍有不属于新 kind 的非空字段 → 40001（要求显式传 extra）。
func reconcileExtra(newKind string, existing map[string]string) (map[string]string, error) {
	raw := make(map[string]any, len(existing))
	for k, v := range existing {
		if v != "" {
			raw[k] = v
		}
	}
	out, err := normalizeExtra(newKind, raw)
	if err != nil {
		return nil, badParam("existing extra does not fit kind %q; provide extra explicitly", newKind)
	}
	return out, nil
}

// fillExtraDefaults 读路径兜底：确保响应 extra 恒为该 kind 的完整形状（老数据缺 key 时补空串）。
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
