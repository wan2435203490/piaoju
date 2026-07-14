package importer

import "strings"

// 系统分类 id（migrations/0002 seed，契约 §3；id 固定 1-11）。
const (
	catFood      int64 = 1  // 餐饮🍜
	catDrink     int64 = 2  // 奶茶🧋
	catTransport int64 = 3  // 交通🚇
	catShopping  int64 = 4  // 购物🛍
	catFun       int64 = 5  // 娱乐🎮
	catDaily     int64 = 6  // 日用🧻
	catMedical   int64 = 7  // 医疗💊
	catOther     int64 = 8  // 其他📦（expense 兜底）
	catSalary    int64 = 9  // 工资💰
	catRedPacket int64 = 10 // 红包🧧
	catOtherIn   int64 = 11 // 其他🪙（income 兜底）
)

// rule 关键词 → 分类。匹配对象是「交易对方 + 商品 + 交易类型/分类」拼接后的小写文本。
type rule struct {
	categoryID int64
	keywords   []string
}

// expenseRules 支出分类规则（顺序敏感：先具体后宽泛，命中即止）。
// 奶茶/咖啡放在餐饮之前——「星巴克咖啡」既含「咖啡」也可能含「餐」，奶茶更具体。
var expenseRules = []rule{
	{catDrink, []string{
		"星巴克", "瑞幸", "喜茶", "奈雪", "蜜雪", "coco", "一点点", "茶百道", "古茗",
		"霸王茶姬", "书亦", "沪上阿姨", "库迪", "manner", "奶茶", "咖啡", "starbucks", "luckin",
	}},
	{catTransport, []string{
		"滴滴", "地铁", "公交", "12306", "铁路", "高铁", "火车", "加油", "中国石化", "中国石油",
		"停车", "etc", "出租", "打车", "哈啰", "青桔", "美团单车", "航空", "机票", "曹操出行", "t3出行",
	}},
	{catMedical, []string{
		"药房", "药店", "大药房", "医院", "门诊", "诊所", "挂号", "体检", "医药", "健康", "口腔",
	}},
	{catFun, []string{
		"电影", "影城", "影院", "猫眼", "淘票票", "游戏", "steam", "网易游戏", "腾讯游戏",
		"ktv", "剧场", "演出", "大麦", "b站", "哔哩哔哩", "爱奇艺", "腾讯视频", "优酷", "网易云音乐", "会员",
	}},
	{catDaily, []string{
		"超市", "便利店", "沃尔玛", "永辉", "山姆", "盒马", "罗森", "全家", "7-eleven", "711",
		"美宜佳", "话费", "充值", "水电", "物业", "日用", "名创优品",
	}},
	{catShopping, []string{
		"淘宝", "天猫", "京东", "拼多多", "唯品会", "苏宁", "小米", "apple", "抖音电商",
		"服饰", "旗舰店", "商城", "购物",
	}},
	{catFood, []string{
		"美团", "饿了么", "餐厅", "餐饮", "food", "饭店", "小吃", "快餐", "麦当劳", "肯德基",
		"kfc", "汉堡", "面馆", "拉面", "火锅", "烧烤", "料理", "食堂", "外卖", "海底捞", "必胜客", "餐",
	}},
}

// incomeRules 收入分类规则（顺序敏感）。
var incomeRules = []rule{
	{catSalary, []string{"工资", "薪资", "薪酬", "劳务", "报酬", "工资卡", "发薪", "奖金"}},
	{catRedPacket, []string{"红包", "转账", "群收款", "亲属卡", "赞赏", "打赏"}},
}

// classify 规则分类：按商户名/商品名/交易类型关键词匹配系统分类。
// 认不出 → 8（支出·其他）/ 11（收入·其他）。分类只是「建议值」，客户端可改（契约 §6.2）。
func classify(direction, counterparty, item, kind string) int64 {
	text := strings.ToLower(counterparty + " " + item + " " + kind)

	rules, fallback := expenseRules, catOther
	if direction == "income" {
		rules, fallback = incomeRules, catOtherIn
	}
	for _, r := range rules {
		for _, kw := range r.keywords {
			if strings.Contains(text, kw) {
				return r.categoryID
			}
		}
	}
	return fallback
}
