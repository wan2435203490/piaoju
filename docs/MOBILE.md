# Android 打包（Wave 5 · Capacitor）

拾光票局 web 端（SvelteKit SPA）用 Capacitor 壳打成 Android App。iOS 暂缓（需 Apple 开发者账号 $99/年）。

## 工程位置

- Capacitor 配置：`web/capacitor.config.ts`
- 原生工程：`web/android/`（`npx cap add android` 生成，随仓库提交；`app/build`、`.gradle`、`assets/public`、`local.properties` 等已 gitignore）
- 原生桥接：`web/src/lib/native/`（相机 `camera.ts`、状态栏/启动图 `shell.ts`）

`appId = local.piaoju.app`（本地占位；上架前换成正式反域名，如 `com.yourorg.piaoju`）。

## 前置环境

- **JDK 21**（必需）：`/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home`。
  **注意不是 17**——`@capacitor/camera` 8.2.1 的 `sourceCompatibility VERSION_21` 强制要求 JDK 21，
  用 JDK 17 会在 `:capacitor-camera:compileDebugJavaWithJavac` 报「Cannot find a Java installation
  matching languageVersion=21」。本机 openjdk@21 是构建时 `brew install openjdk@21` 补装的。
- Android SDK：`~/Library/Android/sdk`（platforms: android-34/35/36，build-tools: 34/35/36）。
  android-35 platform 是补装的（Capacitor 8 的 `compileSdkVersion = 35`）。

构建前设好环境变量：

```bash
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
export ANDROID_HOME=~/Library/Android/sdk
export PATH="$JAVA_HOME/bin:$PATH"
```

`web/android/local.properties` 需含 `sdk.dir=<你的SDK路径>`（已生成，gitignore）。

## 构建流程

```bash
cd web
pnpm mobile:sync          # pnpm build && cap sync android（改完前端每次跑）

# debug APK（免签名，装真机测）
cd android && ./gradlew assembleDebug
# → app/build/outputs/apk/debug/app-debug.apk

# release APK（R8 压缩，未签名）
./gradlew assembleRelease
# → app/build/outputs/apk/release/app-release-unsigned.apk

# 或 Android Studio 图形化
pnpm mobile:open
```

装到真机：

```bash
adb install -r web/android/app/build/outputs/apk/debug/app-debug.apk
```

## 当前产物尺寸（2026-07-15 实测）

| APK | 大小 | 说明 |
|---|---|---|
| debug | 8.1MB | 未压缩，能装真机测 |
| release (unsigned) | 6.5MB | R8 压缩后；**超出 <6MB 目标约 0.5MB**；未签名装不上 |

**release 超标说明**：体积主要来自 dex 里的 androidx + Capacitor 运行时（原生 .so 库仅 0.15MB、
web 资源 0.41MB，都很小，ABI 拆分帮助有限）。收缩手段：
- 出 Android App Bundle（`.aab`，`./gradlew bundleRelease`）交 Play Store 按设备下发，实际下载体积更小
- `android/app/build.gradle` 开 `shrinkResources true`（配合已开的 R8 `minifyEnabled true`）
- 6MB 目标对 Capacitor App 偏乐观，可评估是否放宽到 8MB

## 签名（release 上架前）

`assembleRelease` 出的是**未签名** release。上架需生成 keystore 并配 `android/app/build.gradle` 的 `signingConfigs`：

```bash
keytool -genkey -v -keystore piaoju.keystore -alias piaoju \
  -keyalg RSA -keysize 2048 -validity 10000
```

keystore **不要提交仓库**——放安全处，密码走 CI secret 或本地 `~/.gradle/gradle.properties`。

## 原生能力接入现状

| 能力 | 插件 | 状态 |
|---|---|---|
| 拍票走系统相机 | `@capacitor/camera` | ✅ 原生优先，Web 回退文件选择器（`native/camera.ts`，动态 import 不进 web 包） |
| 状态栏配色 | `@capacitor/status-bar` | ✅ 跟随亮/暗主题（`native/shell.ts`） |
| 启动图 | `@capacitor/splash-screen` | ✅ 纸白底，web 就绪后收起 |
| 文件系统 | `@capacitor/filesystem` | ⚠️ 已装未用（离线附件走 Dexie blob；如需导出到相册再接） |

## 图标与启动图

- **App 图标**：adaptive icon（API 26+）用矢量票根前景 + 纸白底，见
  `android/app/src/main/res/drawable/ic_launcher_foreground.xml` 与
  `mipmap-anydpi-v26/ic_launcher.xml`。任意分辨率清晰，无需图像工具。
- **API <26 回退**：`mipmap-*/ic_launcher.png` 仍是 Capacitor 默认图标——本机无图像工具（magick/rsvg/sharp/PIL 皆无）无法生成品牌 PNG。**待办**：拿一张 1024×1024 品牌图，`npx @capacitor/assets generate` 一次生成全套 PNG 图标 + 启动图。
- **启动图**：纯纸白底（config 的 `backgroundColor`），未放 logo 图；同上，有品牌图后用 assets 工具生成。

## 真机跑起来还差的（重要）

App 能装 ≠ App 能用，还差两步：

1. **后端连接**：App 里 web 资源请求 `/api` 走相对路径，但 App 内没有后端。dev 后端在 `localhost:8080`，手机访问不到 localhost。要么 `capacitor.config.ts` 配 `server.url` 指向同网段电脑 IP（如 `http://192.168.x.x:8080`），要么部署后端到可达地址。
2. **识票 key**：识票端点需服务端配 `PIAOJU_LLM_API_KEY`，且后端要能被手机访问。未配则 App 内识票入口自动隐藏（50001），不影响其他功能。

## 已知未做 / 待办

- 真机走查：装上手机后实测渲染/拍照/暗色/键盘遮挡（APK 结构已验证有效，但没在设备上跑过）
- iOS 工程（等 Apple 账号）
- 品牌 PNG 图标 + 启动图（缺图像工具）
- release 签名配置（缺 keystore）
- 后端连接配置（见上）
- TestFlight / Play Console 上架流程
