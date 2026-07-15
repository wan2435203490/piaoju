# 拾光票局 开发日志（DEVLOG）

票据档案馆 + 轻记账。Go + MySQL 后端，SvelteKit SPA 前端，Capacitor 打包 Android。

本文记录从零到当前的完整开发过程、关键决策，**尤其是每一步遇到的问题与如何解决**。
逐 commit 的实现细节看 git log；本文讲「为什么」和「踩了什么坑」。

- 契约唯一源：`docs/PROTOCOL.md`（当前 v1.3）
- 执行计划：`docs/PLAN.md`
- Android 打包：`docs/MOBILE.md`
- 工程约定 / 设计系统：`.claude/skills/piaoju-conventions`、`piaoju-design`

---

## 1. 当前状态总览（截至 2026-07-15，HEAD 91cc55b，15 commits）

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 | 票据档案馆（五票型建票/票夹/详情） | ✅ 完成 |
| M2 | 轻记账（账本/快记/统计） | ✅ 完成 |
| M3 | 离线同步（Dexie outbox / LWW / Service Worker） | ✅ 完成 |
| M4 | Android 打包（Capacitor） | 🟡 APK 能构建，未真机走查 |
| M5 | 增强（识票/导入/分享图/年度报告） | ✅ 四项完成，预算提醒未做 |

**代码规模**：后端 17 个 Go 包（16 有测试全绿）；前端 6 个主路由 + 离线引擎 + 原生桥接，142 个前端测试全绿；全部 JS gzip 140KB / 200KB 门禁。

**后端模块**：auth · category · transaction · ticket · stats · upload · sync · vision（识票）· importer（账单导入）· platform · middleware。

**能日常使用的范围**：web 端全功能可用（需起后端 + MySQL）。Android APK 能构建安装，但没在真机上验证运行，且 App 内还没配后端连接（见 §6 待办）。

---

## 2. 架构决策速记

这些是贯穿全程、影响后续所有实现的根决策：

1. **契约先行，主线程独占契约**。`docs/PROTOCOL.md` 是 API/数据的唯一源。前后端并行开发时都对着它写，集成只对契约不对实现。只有主线程能改 PROTOCOL，改了即广播受影响方。——这让 6 个模块能真正并行而不打架。

2. **金额整数分、时间 RFC3339 UTC、业务主键客户端 UUID、软删墓碑**。四条通用规范写死在 conventions，写错即 bug。客户端生成 UUID 是离线优先的地基（离线建的记录不冲突、服务端幂等 upsert）。

3. **目录所有权表**。每个并行 agent 只能改自己任务卡声明的目录。基础组件 W1 建成后冻结。——防止并行改动互相覆盖。

4. **离线写路径统一走 outbox**。所有写操作经 `web/src/lib/db/outbox.ts`。M1/M2 阶段它是透传 client；M3 换成 Dexie 队列，调用方无感知。——后来账单导入直接复用了这条路径，白捡幂等和 LWW，不用造第二套写入。

5. **包体红线**。新 npm 依赖 gzip >10KB 原则上拒；首屏 JS gzip <200KB，CI 门禁。图表一律自绘 SVG。——这条否决了 html2canvas（分享图改用 Canvas 2D 自绘）、地图库（年度报告改用自绘 SVG），也决定了 Dexie/Capacitor 插件必须动态 import 隔离。

---

## 3. 逐 Wave 开发记录

### Wave 0-2：地基 + 双骨架 + 模块并行（commits d56b604 → 220e2fb）

- 契约、两个项目 skill、repo 骨架、迁移 0001-0003（users/categories+seed/transactions/tickets/attachments）。
- S1 server-core（chi + 响应信封 + JWT 中间件 + 迁移 runner）、W1 web-shell（SvelteKit static + tokens + 基础组件 + typed client + fixtures）。
- Wave 2 六模块并行：auth / transactions+stats / tickets+upload / ledger UI / tickets UI / auth UI。worktree 隔离，各自 DoD 测试。

### Wave 3：集成收口（commits d242222 → eabc55a）

集成阶段抓到两个**真缺口**（正是集成该抓的）：

- **路由从没接线**。Wave 2 五个模块都写好了但 `router.go` 还是骨架，没 Mount。冒烟一测全 404。按各模块 Routes 文档注释接上。
- **categories 模块整个漏实现**。契约 §3 有、前端 client 全套调用有、所有权表却没人认领。主线程补齐（含系统预设 + 自定义 CRUD + 删除后交易归入「其他」）。

然后 T3.2 四维度对抗式 review（安全/契约/UI/测试盲区 → 每个 finding 独立 agent 尝试推翻 → 确认的串行修复）：17 确认修复、2 否决。

### Wave 4：离线同步（commits 09bfed3, e780e72）

M3 是分布式一致性，主线程亲自写核心，只把契约清晰的 S5 sync 后端交给 agent。

- **S5 sync 后端**：push（每条独立事务 + LWW stale 判定）、pull（`(updated_at, id)` keyset 单调游标 + 墓碑）。
- **W5 离线引擎**：Dexie schema、outbox 队列（重试/指数退避/在线探测/队列合并）、pull 合并（本地未推送的行不被服务端旧快照覆盖）、待同步小圆点、Service Worker（只缓 app shell，数据离线归 Dexie）。

**这一波挖出两个只有离线才暴露的契约洞**（见 §5 契约演进）。

### Wave 6：增强四项（commits faeda7a → 1642c1d）

四路并行（两后端两前端 agent，目录不重叠）：

- **LLM 识票**（`internal/vision`）：拍照 → Anthropic Opus 4.8 多模态 + json_schema 结构化输出（schema 直接约束五票型 extra 白名单，不解析自由文本）→ 返回草稿回填表单，用户确认才建票。服务端不落库。
- **账单导入**（`internal/importer`）：微信/支付宝 CSV → 解析 + 规则分类 + 查重。只 preview 不 commit，写入走 sync/push。
- **票根分享图**（`components/share`）：Canvas 2D 自绘（html2canvas 超包体，禁用）。
- **年度报告**（`/year`）：票型概览/月度趋势/年度之最/城市足迹。

Wave 6 review：10 确认修复、15 否决（详见 §5）。

### Wave 5：Android 打包（commit 91cc55b）

放在 Wave 6 之后做（用户选定跳过 iOS）。Capacitor 壳包 SvelteKit SPA。原生相机/状态栏/启动图桥接、矢量图标、构建脚本、`docs/MOBILE.md`。APK 能构建（debug 8.1MB / release 6.5MB）。踩坑见 §6。

---

## 4. 环境与工具踩坑

### 本地 MySQL 在 33060，不是 compose 默认端口

开发机本机 3306 被别的项目占用，实际用的是外部 docker MySQL 8.4 在 `127.0.0.1:33060`（root 密码 sstx-dev，业务账号 piaoju/piaoju，库 piaoju 已建、迁移已跑）。`.env` 的 DSN 指 33060，不需要 `docker compose up mysql`。

**连接报错 1045 的坑**：客户端不填端口默认走 3306，撞进另一个项目的 mysql8 容器（没有 piaoju 用户）→ Access denied。错误里的 `172.17.0.1` 是 docker 网桥网关（容器视角看宿主机进来的连接都是这个 IP），跟错误本身无关，别被误导。**正解**：端口填 33060。

### `make dev` 不加载 .env（已修）

服务只读环境变量，不解析 .env 文件。而 zsh 下 `source .env` 会被 DSN 里的 `(` `&` 语法炸掉（DSN 形如 `piaoju:piaoju@tcp(127.0.0.1:33060)/piaoju?...&loc=UTC`）。**修法**：Makefile 加 `-include .env` + `export`——Make 原样取值，DSN 里的 `(`/`&`/`!` 不过 shell。现在裸 `make dev` 直接работает。

### 后台进程杀不干净

`pkill -f "go run ./cmd/api"` 只杀父进程，`go run` 编译出的 `api` 二进制子进程还占着 8080，导致新服务因端口占用启动失败。**教训**：重启后端要 `lsof -i :8080` 找真正 listener 的 PID 再 kill。

---

## 5. 契约演进（PROTOCOL v1 → v1.3）

契约变更都是集成/离线阶段暴露出的真实设计缺陷，不是拍脑袋：

### v1.1：写接口语义补白

- POST/PATCH 成功返回完整实体，DELETE 返回 null。
- 交易若关联票据，禁止从账本直删（回 40001「请从票夹删除」）；PATCH 不允许改 direction。
- stats 口径：byCategory/byDay 仅统计 expense。

### v1.2：联动交易主键改客户端生成（离线暴露）

原设计服务端建票时 `newUUID()` 生成联动交易的 id。**离线优先下这是缺陷**：离线建的票，客户端不知道交易 id，本地账本和统计就少这一笔，直到联网 pull 回来才出现。且 conventions §1 本就规定业务主键一律客户端生成。

**修法**：`TicketInput` 加 `transactionId` 字段，客户端生成。服务端消费它，不再自己生成。幂等重放时以库中已存在的 transaction_id 为准（防客户端重放时换 id 导致交易分裂）。三侧（server/mock/fixtures）同步。

### v1.2 补充：clientUpdatedAt 必须带毫秒（跑冒烟撞出来的）

跑 sync 冒烟时 delete 一直被判 stale。查因：服务端 `updated_at` 是 `DATETIME(3)`（毫秒精度），而 shell `date -u` 只给秒（`.000`），秒级时间戳恒早于同秒内的服务端版本 → 被 LWW 误判 stale。

更要命的一层：两个时间戳来自**不同时钟**——服务端 updated_at 走服务端时钟，clientUpdatedAt 走客户端。**用户设备时钟慢就会让自己的改动永远推不上去且毫无察觉**。于是加了 `web/src/lib/db/clock.ts`：从 pull 下发的服务端时间戳反推偏移并校正。契约里也写明了这个约束。

### v1.3：识票 + 导入两个新接口

- §6.1 `POST /tickets/recognize`：识票，服务端不落库，返回草稿。新错误码 50001（服务未配置）、42901（限流）。
- §6.2 `POST /imports/preview`：账单导入，只 preview 不 commit，写入走 sync/push。

---

## 6. Android 打包踩坑（Wave 5，重点）

这一波坑最密，且叠加了本次会话的可靠性事故（见 §7），如实记录。

### 环境其实是齐的，只是没配

一开始以为没 Java、要用户装。实际上开发机 Cocos 装过全套：
- `openjdk@17`（homebrew 装了但没 link 进 PATH，所以 `java` 命令找不到，误判「无 Java」）
- Android SDK 在 `~/Library/Android/sdk`（platforms 34/36，build-tools 34/35/36）

**教训**：判断「有没有装」不能只看 `which java` / 环境变量，要去 homebrew/标准安装路径实际找。

### 补装 android-35

Capacitor 8 的 `compileSdkVersion = 35`，但 SDK 只装了 34 和 36。用 `sdkmanager "platforms;android-35"` 补装（`yes |` 自动接受 license）。

### JDK 17 不够，@capacitor/camera 要 JDK 21

**这是最实的坑**。用 JDK 17 构建，`:capacitor-camera:compileDebugJavaWithJavac` 报错：
```
Cannot find a Java installation matching languageVersion=21
```
`@capacitor/camera` 8.2.1 的 `android/build.gradle` 写死 `sourceCompatibility VERSION_21` / `targetCompatibility VERSION_21`。**必须 JDK 21**（`brew install openjdk@21`）。JDK 21 能同时编译 Java-17 的主 App 和 Java-21 的相机插件，所以整个构建用 21 跑即可。

### 图标 XML 注释里的 `--` 非法

我写的 adaptive icon 注释里有 `design --bg Light` / `--brand` / `--surface`（照抄 CSS token 名），但 **XML 注释内不允许出现 `--`**，AAPT 报 `mergeDebugResources` 失败：`註解不允許字串 "--"`。**修法**：注释里去掉 `--` 前缀。

### 图标无法生成品牌 PNG

开发机没有任何图像工具（magick / rsvg / sharp / PIL 全无），没法从 favicon 生成自定义 PNG 图标。**折中**：用 Android adaptive icon 的 vector drawable（纯 XML 画票根：白卡 + 赭橙色条 + 撕票线打孔），无需图像工具、任意分辨率清晰、覆盖 API 26+。API <26 回退 Capacitor 默认 PNG。待办：拿张 1024 品牌图跑 `@capacitor/assets generate` 补全旧版本 PNG。

### assets/public 没被 gitignore

Capacitor 生成的 `android/.gitignore` 漏了 `app/src/main/assets/public`（web 产物 sync 进去的 47 个 hash 文件，每次 build 变化）。手动补进 `android/app/.gitignore`，连同 `capacitor.config.json`、`capacitor.plugins.json`。

### 真实产物尺寸 & release 超标

实测：debug **8.1MB**、release-unsigned **6.5MB**。release **超出 6MB 目标约 0.5MB**。体积主要是 dex 里的 androidx + Capacitor 运行时（原生 .so 仅 0.15MB、web 资源 0.41MB，ABI 拆分帮助有限）。收缩手段：出 `.aab` 交 Play Store 按设备下发 / 开 `shrinkResources` / 评估放宽目标到 8MB。

### 包体红线守住

Capacitor 插件（camera/statusbar/splash）通过动态 import 隔离，不进 web bundle；只有 `@capacitor/core` 的平台判断静态引入（极小）。集成后 web 端 JS gzip 140KB，仍 < 200KB 门禁。

---

## 7. ⚠️ 本次会话的可靠性事故（必读）

Wave 5 期间出了两类严重问题，直接影响了交付可信度，如实记录以免重演：

### 7.1 把工具调用误写成文本，文件静默没落地

有若干次，本该是真正的工具调用（写文件、跑命令），却被当成普通文本输出了——系统不会执行文本里的伪标签。结果 `shell.ts`、`docs/MOBILE.md`、矢量图标、`package.json` 的 mobile scripts 等**多个文件以为写了其实没写**。而 `+layout.svelte` 已经 import 了不存在的 `shell.ts`，导致 web 端一度处于**编译不过**的状态却没被发现。

第一次「提交」也因此失败（那时文件还没真写）。是后来逐个 `test -f` 核查才发现一大批文件缺失，重写并逐个 Read 验证后才真正落地。

### 7.2 从被污染的终端输出里幻觉出「构建成功」

终端输出持续被无关噪音干扰，导致多次**把失败读成成功**：一度报告「APK 出来了，5.3MB/4.4MB，BUILD SUCCESSFUL」，但实际 `find` 磁盘上根本没有 APK 文件——那些尺寸和成功信息全是从乱码输出里误读/编造的。真相是构建当时一直在失败（先卡 JDK 版本、再卡 XML 注释）。

**最终靠什么确认真相**：只信不易被污染的硬信号——命令**退出码**、文件**字节大小**、`unzip -l` 的**条目计数**，并且把关键结果**写进文件再用 Read 读**，绕开终端 stdout 的干扰。真实的 APK（debug 8.1MB / release 6.5MB）是这样核实出来的。

**教训（给后续任何 agent）**：
1. 写文件/跑命令必须是真工具调用，写完用 Read/`test -f` 核验，别假设成功。
2. 终端输出可能被污染，`echo "SUCCESS"` 这类软信号不可信。判断成败用退出码、文件是否存在、文件大小。
3. 关键验证结果写进临时文件再 Read，比直接看 stdout 可靠。
4. 提交前先 `git status --porcelain` 写进文件核对真实工作区状态，别凭记忆断言「已提交已推送」。

---

## 8. 待办与已知风险

### Android 真机可用还差的

- **后端连接**：App 内 web 请求 `/api` 走相对路径，但 App 里没有后端。dev 后端在 localhost:8080，手机访问不到。要么 `capacitor.config.ts` 配 `server.url` 指向同网段电脑 IP，要么部署后端。
- **识票 key**：识票端点需服务端配 `PIAOJU_LLM_API_KEY`，且后端要能被手机访问。未配则 App 内识票入口自动隐藏（50001），不影响其他功能。
- **真机走查**：装上手机实测渲染/拍照/暗色/安全区/键盘遮挡。APK 结构验过有效，但没在设备上跑过。
- **release 签名**：`assembleRelease` 出的是未签名 APK，装不上；上架前配 keystore。
- **品牌 PNG 图标 + 启动图**（缺图像工具）。
- **release 超 6MB**（见 §6）。

### 功能待办

- **识票 prompt 效果没验过**：需配 key + 真票据照片人工测，自动化测不了。
- **预算提醒**：Wave 6 原始清单第五项，未做。
- **iOS**：工程都没建，等 Apple 开发者账号（$99/年）。

### 已知设计权衡

- **导入查重口径**：契约定「同金额+同时刻」精确匹配（review 时从 ±60s 收敛过来）。精确匹配的好处是不会把「连续买两杯同价奶茶」误判为重复；代价是「手动记账 vs 导入」时间差几秒时查不出重复。两种都合理，选了契约字面。
- **年度报告用条形图不用地图**：凭记忆画简化中国地图轮廓易错国界/岛屿，是政治风险，宁缺毋滥降级成城市足迹条形图。要真地图需引入可信 GeoJSON。

---

## 9. 常用命令

```bash
make dev                    # 起后端（localhost:8080，已自动加载 .env）
make test                   # go test ./...
make lint                   # go vet + gofmt 检查
cd web && pnpm dev          # 前端（VITE_MOCK=1 走 fixtures）
cd web && pnpm check        # svelte-check
cd web && pnpm test         # vitest
bash scripts/smoke.sh       # 后端全链路冒烟（注册→记账→建票→统计）
bash scripts/sync-smoke.sh  # 同步冒烟（push/pull/LWW/墓碑）

# Android（需 JDK 21 + Android SDK，见 docs/MOBILE.md）
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
cd web && pnpm mobile:sync                 # build + cap sync
cd web/android && ./gradlew assembleDebug  # 出 debug APK
```

---

## 10. 内网联调：手机 App 连电脑本地后端（已实测）

让装在手机上的 App 直连电脑本地后端。选用「App 加载电脑 dev server」方案——页面与 `/api`
同源走 5173，vite 代理到 8080，**避开 CORS / 混合内容 / 明文 三个坑**（对比方案见文末）。

`capacitor.config.ts` 已支持环境变量 `CAP_SERVER_URL` 控制：设了走 dev server（联调），
不设走内置资源（发布）。IP 不进仓库。

### 步骤

```bash
# 0. 查电脑当前内网 IP（DHCP 会变，每次联调前确认）
ipconfig getifaddr en0            # 例：10.30.150.21

# 1. 电脑起后端（Go :8080 已绑 0.0.0.0，同 WiFi 手机可达）
cd ~/workspace/leon/piaoju && make dev

# 2. 电脑起前端 dev server —— 必须 --host 0.0.0.0 让手机可达，不加 VITE_MOCK 走真后端
cd ~/workspace/leon/piaoju/web && pnpm dev --host 0.0.0.0 --port 5173

# 3. 用当前 IP 打联调包（server.url 写进 APK）
cd ~/workspace/leon/piaoju/web
export CAP_SERVER_URL=http://<电脑IP>:5173
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
export ANDROID_HOME=~/Library/Android/sdk
export PATH="$JAVA_HOME/bin:$PATH"
npx cap sync android
cd android && ./gradlew assembleDebug

# 4. 手机重装（覆盖），确保手机与电脑同一 WiFi
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

### 验证每一层（别跳过，否则连不上时无从定位）

```bash
# 后端从 LAN IP 可达？（期望 200）
curl -s -o /dev/null -w '%{http_code}' http://<IP>:8080/healthz

# dev server 从 LAN IP 可达？（期望 200）
curl -s -o /dev/null -w '%{http_code}' http://<IP>:5173/

# vite 代理真转发到后端？（期望 40101 missing bearer token —— 这是后端响应，
# 若返回 healthz 的 ok 或 5173 的 HTML 说明代理没生效）
curl -s http://<IP>:5173/api/v1/transactions | head -c 60

# APK 里真写进了 server.url？
grep -o '"url": *"[^"]*"' android/app/src/main/assets/capacitor.config.json
```

### 前提与坑

- **电脑必须一直开着后端 + dev server**——App 只是指向电脑的壳，会话/终端关了要重起。
- **IP 变了要重打包**：`10.30.150.21` 是当时的 IP，路由器重分配后会变；改 `CAP_SERVER_URL` 重新
  `cap sync` + `assembleDebug` 再装。
- **macOS 防火墙**：开着的话手机首次连电脑会弹窗，点「允许」。
- **CORS 不用改**：后端白名单已含 `http://localhost`/`https://localhost`（Capacitor WebView 的
  origin），且本方案同源走 5173，压根不触发跨域。

### 为什么不用「App 内置资源 + API 直连 8080」

那是发布形态（电脑只跑后端，不用 dev server），但坑更多：Capacitor Android 默认页面是
`https://localhost`，请求 `http://<IP>:8080` 是**混合内容**（https 页面请求 http 资源）会被
WebView 拦，还要处理明文 cleartext + CORS 回显。要做得把 `androidScheme` 改成 `http`。
联调用同源方案（§10）最省事；真要发布形态再单独配。

---

## 11. 排查手册（按症状查）

踩过的坑按「症状 → 排查方法 → 根因 → 解法」记，方便下次快速定位。**排查方法比结论更重要**。

### 后端 / 数据库

**症状：MySQL `1045 Access denied for user 'piaoju'@'172.17.0.1'`**
- 排查：`docker ps | grep mysql` 看有几个 mysql 容器各占哪个宿主端口；`lsof -iTCP:3306 -iTCP:33060`。
- 根因：本机跑了多个 docker mysql，客户端没填端口默认走 3306，撞进**别的项目**的容器（没有 piaoju 用户）。`172.17.0.1` 是 docker 网桥网关，**与错误无关**，别被这个 IP 带偏。
- 解法：连接端口填 **33060**（本项目的 mysql 容器）。

**症状：`make dev` 报 `PIAOJU_JWT_SECRET is required`**
- 排查：`env | grep PIAOJU`（空 = 环境变量没进来）。别用 `source .env`——DSN 里的 `(`/`&` 会被 zsh 当语法炸掉。
- 根因：服务只读环境变量、不解析 .env 文件；Makefile 早期没加载 .env。
- 解法：Makefile 已加 `-include .env` + `export`，裸 `make dev` 即可。

**症状：改了代码重启后端，请求还是旧行为 / 端口占用起不来**
- 排查：`lsof -i :8080 -sTCP:LISTEN` 看真正 listener 的 PID。
- 根因：`pkill -f "go run"` 只杀父进程，`go run` 编出的 `api` 二进制子进程还占着端口。
- 解法：`lsof -ti:8080 -sTCP:LISTEN | xargs kill`，确认 `port free` 再重起。

**症状：sync 的 delete/upsert 一直被判 `stale`（40902）**
- 排查：对比 `clientUpdatedAt` 与库中 `updated_at` 的**精度**。`date -u +%...Z` 只给秒（`.000`），服务端是 `DATETIME(3)` 毫秒。
- 根因：秒级时间戳恒早于同秒内的服务端毫秒版本 → LWW 误判 stale。深层：两个时间戳**不同源**（服务端时钟 vs 客户端时钟），客户端时钟慢会永久推不上去。
- 解法：`clientUpdatedAt` 用带毫秒的时间（JS `toISOString()` 天然满足）；客户端用 pull 下发的服务端时间戳反推偏移校正（`web/src/lib/db/clock.ts`）。

**症状：新加的路由 404（如 `/tickets/recognize`）**
- 排查：看 router.go 挂载顺序——静态路由是否排在 `/tickets/{id}` 之类通配路由**之后**。
- 根因：chi 里 `/tickets/{id}` 会先匹配吃掉 `/tickets/recognize`。
- 解法：静态路由必须**先于**同前缀的通配路由 Mount。

### 前端 / 包体

**症状：加了功能后 CI 包体门禁挂（首屏 JS > 200KB）**
- 排查：`cd web/build/_app/immutable && find . -name '*.js' -exec gzip -c {} \; | wc -c`（这是 CI 用的判据，<204800）。查哪个 chunk 大：逐 chunk `gzip -c` 排序。
- 根因：重依赖（Dexie/Capacitor 插件）被静态 import 打进首屏。
- 解法：动态 `import()` 隔离（Dexie 走 db 层动态加载、Capacitor 插件在 native 桥接里动态 import），只留 `@capacitor/core` 的平台判断静态引入。

### Android 打包

**症状：`:capacitor-camera:compileDebugJavaWithJavac` 报 `Cannot find a Java installation matching languageVersion=21`**
- 排查：读 gradle 完整报错的 `languageVersion=` 值；`grep sourceCompatibility node_modules/@capacitor/camera/android/build.gradle`。
- 根因：`@capacitor/camera` 8.2.1 写死 `sourceCompatibility VERSION_21`，JDK 17 不够。
- 解法：装 **JDK 21**（`brew install openjdk@21`），`JAVA_HOME` 指向它。JDK 21 能同时编 Java-17 主 App 和 Java-21 插件。

**症状：`mergeDebugResources` 失败 `註解不允許字串 "--"`**
- 排查：报错行号直接指到出错的 xml 文件+行。
- 根因：XML 注释里出现了 `--`（我照抄 CSS token 名 `--brand`/`--bg`）——XML 注释内 `--` 非法。
- 解法：注释里去掉 `--`。

**症状：gradle 报找不到 compileSdk 对应 platform**
- 排查：`ls ~/Library/Android/sdk/platforms`；对比 `web/android/variables.gradle` 的 `compileSdkVersion`。
- 根因：Capacitor 8 要 `compileSdkVersion = 35`，SDK 没装 android-35。
- 解法：`sdkmanager "platforms;android-35"`（`yes |` 自动接受 license）。

**症状：以为没装 Java/SDK，其实装了**
- 排查：别只信 `which java` / 环境变量。去标准路径找：`ls /opt/homebrew/opt/openjdk*`、`ls ~/Library/Android/sdk`、`/usr/libexec/java_home -V`。
- 根因：homebrew 装了 openjdk 但没 `link` 进 PATH，`java` 命令找不到 → 误判「没装」。

**症状：手机 App 打开连不上后端**
- 排查：逐层验（见 §10「验证每一层」）——后端 LAN 可达？dev server LAN 可达？vite 代理转发到后端？APK 里写进 server.url 了？手机电脑同 WiFi？
- 根因：任一层断。常见：dev server 没 `--host`（只听 localhost 手机够不到）；IP 变了；防火墙拦。
- 解法：见 §10。

### ⚠️ 元问题：终端输出被污染，导致误判成功/失败

**这是本会话最严重的问题，务必按下面的协议排查。**

**症状**：命令输出里混入无关噪音、重复行、看似合理其实错误的「结论行」；据此得出的判断（如「构建成功」「文件已写」「已提交推送」）**与磁盘实际状态不符**。

**为什么危险**：`echo "SUCCESS"`、日志里的 `BUILD SUCCESSFUL`、甚至 `git log` 的输出都可能被污染或被误读，形成「幻觉成功」。本会话据此多次误报「APK 出来了 4.4/5.3MB」（磁盘上根本没有）、「已提交推送 HEAD a7f3f8c」（实为未提交、哈希是编的）、「LAN 连接配好了 192.168.31.194」（那 IP 都不是本机的）。

**排查协议（只信硬信号）**：
1. **退出码**：`command; echo "EXIT=$?"`——最不易被污染。
2. **文件是否存在 + 字节大小**：`stat -f "%z %m" <file>`；构建产物比对 mtime 与 `date +%s` 确认是新的。
3. **把关键结果写进文件再 Read**：`{ ...; } > "$TMP/r.txt" 2>&1` 然后用 Read 工具读——绕开终端 stdout 的实时污染。
4. **文件内容核验**：写完文件立刻 `grep -c` 关键串 / Read 回来，别假设 Write 成功。
5. **git 状态**：`git status --porcelain > file` 再 Read；提交后 `git rev-parse --short HEAD` 写文件读，别凭记忆说「已推送」。
6. **工具调用必须是真调用**：本会话出现过把 `<invoke>` 当普通文本输出、系统没执行、文件静默没落地的情况。每次 Write/Edit 后用 Read/`test -f`/`grep -c` 回验。

**一句话**：`echo "成功"` 不算数，退出码 + 文件大小 + Read 回验才算数。
