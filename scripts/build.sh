#!/usr/bin/env bash
# 「慎始」一键构建：前端产物 → 内嵌进 Go 二进制。
# 产出一个可执行文件 bin/shenshi，运行时只需要它 + 一个 SQLite 数据文件。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 工具链版本约定：Node 24 / Go 1.27（与 .nvmrc、web/package.json engines、Dockerfile 一致）。
# 两段查找都支持环境变量覆盖：NODE_BIN / GO_BIN。

# ---------- Node 24 ----------
node_major() { "$1" -v 2>/dev/null | sed 's/^v\([0-9]*\).*/\1/'; }

if [ -z "${NODE_BIN:-}" ]; then
  # 依次尝试：NODE_BIN → PATH → nvm 默认版本 → Homebrew / usr-local 的 node@24。
  # 只看「大版本是不是 24」，不锁小版本，避免升个补丁就要改脚本。
  for cand in "$(command -v node 2>/dev/null || true)" \
              "${NVM_DIR:-$HOME/.nvm}"/versions/node/v24*/bin/node \
              /opt/homebrew/opt/node@24/bin/node \
              /usr/local/opt/node@24/bin/node; do
    [ -n "$cand" ] && [ -x "$cand" ] || continue
    [ "$(node_major "$cand")" = "24" ] || continue
    NODE_BIN="$cand"
    break
  done
fi

if [ -z "${NODE_BIN:-}" ] || [ ! -x "${NODE_BIN:-}" ]; then
  echo "找不到 node 24（当前 PATH 上的是 $(node -v 2>/dev/null || echo '无')）。" >&2
  echo "请安装 Node 24（nvm install 24），或显式指定：NODE_BIN=/path/to/node24 $0" >&2
  exit 1
fi

# 让 npm / npx 与 NODE_BIN 同源，避免「node 是 24、npm 却来自别的安装」。
export PATH="$(dirname "$NODE_BIN"):$PATH"

# ---------- Go 1.27 ----------
# Go 常见安装位置兜底（GUI 启动的终端常常没有配好 PATH）
if [ -z "${GO_BIN:-}" ]; then
  if command -v go >/dev/null 2>&1; then
    GO_BIN="$(command -v go)"
  elif [ -x /opt/homebrew/bin/go ]; then
    GO_BIN=/opt/homebrew/bin/go
  elif [ -x /usr/local/go/bin/go ]; then
    GO_BIN=/usr/local/go/bin/go
  else
    echo "找不到 go，可显式指定：GO_BIN=/path/to/go $0" >&2
    exit 1
  fi
fi

say() { printf '\033[2m▸\033[0m %s\n' "$1"; }

say "node $("$NODE_BIN" -v)（$NODE_BIN）"
say "go   $("$GO_BIN" version | awk '{print $3}')（$GO_BIN）"

# 1) 前端
cd "$ROOT/web"
if [ ! -d node_modules ]; then
  say "安装前端依赖"
  npm install
fi
say "构建前端（vite）"
npm run build

# 2) 产物落到 server/dist，供 go:embed 使用
say "同步前端产物到 server/dist"
rm -rf "$ROOT/server/dist"
mkdir -p "$ROOT/server/dist"
cp -R "$ROOT/web/dist/." "$ROOT/server/dist/"

# 3) Go 二进制
cd "$ROOT/server"
mkdir -p "$ROOT/bin"
say "构建后端（go build）"
"$GO_BIN" build -trimpath -ldflags "-s -w" -o "$ROOT/bin/shenshi" .

say "完成：$ROOT/bin/shenshi"
printf '运行：%s\n' "cd $ROOT && ./bin/shenshi -addr :8787"
