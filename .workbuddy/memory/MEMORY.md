# 慎始 · 项目约定

## 是什么
任务管理应用（复刻滴答清单核心能力），名字取自《礼记》「慎始而敬终」。Go + React，单二进制交付。

## 工具链版本（唯一口径）
**Node 24 / Go 1.27 / Alpine 3.24**，四处声明必须同步：`.nvmrc`、`web/package.json` 的 `engines`、`server/go.mod` 的 `go` 指令、`Dockerfile` 的基镜像。
- ⚠️ **别直接信 PATH 上的 node**：沙箱/托管环境里 PATH 首位可能是更旧的托管版（如 v22），
  用户真实 node 在 nvm（`~/.nvm/versions/node/v24*/bin`）。`scripts/build.sh` 已内置解析：只认大版本 24，命中后把其目录前置到 PATH 让 npm 同源。
  排查工具链问题一律先跑 `./scripts/build.sh`，它开头会打印实际用到的 node/go 版本。
- `go env GOPROXY` 是 `https://goproxy.cn,direct`（公网走 `proxy.golang.org` 会 TLS 超时）。**改 GOPROXY 前先 `go env` 确认**。
- 改了 `web/package.json` 后必须 `npm install` 让 lockfile 跟上，否则 Docker 的 `npm ci` 会失败；用 `npm ci --dry-run` 零副作用体检。
- 本机无 docker：验证镜像靠 ① `CGO_ENABLED=0 GOOS=linux GOARCH=amd64|arm64 go build` 出静态 ELF ② 查 Docker Hub registry API 确认 tag 存在。

## 常用命令
```bash
./scripts/build.sh                                   # 前端 → server/dist → go build → bin/shenshi（并打印 node/go 版本）
nvm use                                              # 读 .nvmrc 切到 Node 24
./bin/shenshi                                        # 默认 :8787，数据 data/shenshi.db（SHENSHI_DB 可覆盖）
python3 scripts/smoke.py                             # 后端端到端 184 项
node scripts/ui-smoke.mjs                            # 真实 Chromium UI 冒烟 78 项（截图到 /tmp/shenshi-shots）
SHENSHI_TOKEN=xxx ./bin/shenshi                      # 设了口令 = 全站门禁；不设 = 现状（本地自用）
cd web && npx tsc --noEmit                           # 前端类型检查
cd server && go vet ./...                            # 后端静态检查
```
`go` 常不在 PATH，用 `/opt/homebrew/bin/go`，脚本已做兜底；curl 本机要加 `--noproxy '*'`。

## 技术约定
- 后端只用标准库 `net/http`（1.22+ 路由语法）+ `modernc.org/sqlite`（纯 Go，禁 cgo）。新增依赖前先确认是否真有必要。
- 前端不引入第三方 UI / 图标库：图标一律手写 SVG 放 `web/src/components/icons.tsx`，基础件放 `ui.tsx`。
- 设计令牌集中在 `web/src/index.css`（宣纸/松烟墨/朱砂印三色，浅深主题，accent 切换）。颜色走 CSS 变量，不写死色值。
- 前端文案与典籍引文集中在 `web/src/lib/quotes.ts`；日期计算统一走 `web/src/lib/date.ts`（本机时区，不引入 dayjs 之类）。
- 状态集中在 `web/src/store/AppStore.tsx`：写操作用乐观更新，随后 220ms 去抖 `reconcile()` 对账并刷新角标；`version` 自增供自持数据的视图（日历/统计）重新拉取。

## 数据层铁律
- **列表查询与计数查询不共用 where()**：`where()` 用于列表（智能清单保留「今日已完成」并沉底到 done 分区），`whereCount()` 用于角标（只算未完成）。两者混用会导致「勾掉的任务凭空消失」或「角标虚高」。
- 时间序列化统一 `model.Now()`（本地时区 RFC3339），比较用同格式字符串前缀/大小比较，不要用 SQLite 的 `date('now')`（那是 UTC）。
- PATCH 语义靠 `Opt[T]` 三态：字段缺席 = 不改，显式 `null` = 清空。新增可空字段时照此实现。
- 校验失败返回 `ValidationError`（→ 400），业务禁止返回 `ForbiddenError`（→ 403），不要用 `fmt.Errorf` 兜底（会变成 500）。
- 允许空请求体的接口用 `decodeOptional`（如打卡缺省取今天）；`decode` 遇空 body 会报 EOF。
- DELETE 处理器统一返回 200 + `{"ok":true,"id":...}`，不要用 204。

## 加一个功能的标准路径
模型 → `store/*.go`（表结构加在 `store.go` 的 `schema` 常量里，纯 `CREATE TABLE IF NOT EXISTS`）→
`api/handlers_*.go` + `api.go` 路由 → 导出/导入同步进 `backup.go` 的 `ExportBundle` →
`web/src/types.ts` → `api/client.ts` → 组件 → `App.tsx` 视图分支与快捷键 + 侧栏 `VIEWS`/快捷键说明 →
后端 `scripts/smoke.py` 加一节 + UI `scripts/ui-smoke.mjs` 加一节。
UI 断言尽量依赖 `data-*` 钩子（`data-task-row` / `data-habit-row` / `data-habit-cell` / `data-cal-mode` …），
比 CSS 类名与 `getByRole(name)` 稳得多（后者对「中文+ASCII 混排」的可访问名匹配不可靠）。

## 鉴权与部署约定
- 门禁开关就是 `SHENSHI_TOKEN` 是否为空：空 = 不启用（本地/内网），非空 = `gate` 包住整个 mux。
- `gate` 白名单只有 `/api/health` 与 `/api/auth/*`；新增**需要免登录**的接口必须同步加进白名单，否则登录页自己会 401。
- 凭据三通道 Cookie → `Authorization: Bearer` → `?token=`；Cookie 必须 HttpOnly + SameSite=Lax。
- 口令比对一律 `subtle.ConstantTimeCompare`，不要用 `==`。
- 容器里 `TZ` 必须显式设置（compose 默认 `Asia/Shanghai`）：跨日判定全靠本地时区，UTC 会让日视图/打卡错位。
- 部署密钥只走 `.env`（compose 里写 `${SHENSHI_TOKEN:?}` 强制必填），任何情况下不把口令写进 `docker-compose.yml`/代码。

## 交互约定
- 中文优先，文案克制、不喊口号，理念靠典籍引文与仪式节点表达。
- 破坏性操作（删除清单/标签/批量删除）必须走 `confirm()` 二次确认。
- 快捷键集中在 `App.tsx`：`N` 新建 / `/` 搜索 / `T` 今天 / `I` 收集箱 / `C` 日历 / `Q` 四象限 / `B` 看板 / `H` 习惯 / `S` 统计 / `Esc` 退出。新增快捷键要同步更新侧栏「外观与设置」里的说明列表。
- 清空搜索必须回到进入搜索前的清单（`preSearch` ref），不要停留在搜索态。
- 覆盖层用 `window` 自定义事件解耦：`shenshi:morning` / `shenshi:review` / `shenshi:focus`，由 `Overlays.tsx` 统一监听。

## 交付纪律
任何改动后至少跑：`tsc --noEmit` + `go vet ./...` + `python3 scripts/smoke.py`；涉及界面再跑 `node scripts/ui-smoke.mjs`。
