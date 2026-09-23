#!/usr/bin/env bash
# 「慎始」一键构建：前端产物 → 内嵌进 Go 二进制。
# 产出一个可执行文件 bin/shenshi，运行时只需要它 + 一个 SQLite 数据文件。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 工具链版本**不写死在这里**：Node 的大版本从 .nvmrc 读，Go 交给 server/go.mod 的 go 指令去要求。
# 于是升级版本只需要改声明文件（.nvmrc / web/package.json 的 engines / Dockerfile / server/go.mod），
# 不必回头动脚本 —— 大版本刻意不冻结，脚本里也就没有需要跟着改的数字。
# 两段查找都支持环境变量覆盖：NODE_BIN / GO_BIN。

# ---------- Node（大版本见 .nvmrc）----------
node_major() { "$1" -v 2>/dev/null | sed 's/^v\([0-9]*\).*/\1/'; }

REQ_MAJOR="$(sed -n 's/^v\{0,1\}\([0-9][0-9]*\).*/\1/p' "$ROOT/.nvmrc" | head -1)"
if [ -z "$REQ_MAJOR" ]; then
  echo "读不出 $ROOT/.nvmrc 里的 Node 大版本。" >&2
  exit 1
fi

# 候选依次为：PATH → nvm → Homebrew / usr-local（含未带版本号的 node，即当前默认大版本）。
# 只看大版本，不锁小版本，避免升个补丁就要改脚本。
node_candidates() {
  command -v node 2>/dev/null || true
  ls -1d "${NVM_DIR:-$HOME/.nvm}"/versions/node/v*/bin/node 2>/dev/null | sort -Vr || true
  echo "/opt/homebrew/opt/node@${REQ_MAJOR}/bin/node"
  echo "/opt/homebrew/opt/node/bin/node"
  echo "/usr/local/opt/node@${REQ_MAJOR}/bin/node"
  echo "/usr/local/opt/node/bin/node"
}

# want=exact 要求大版本恰好相同；want=newer 接受更高的大版本（本机没装要求的大版本时的退路）。
pick_node() {
  local want="$1" cand maj
  while IFS= read -r cand; do
    [ -n "$cand" ] && [ -x "$cand" ] || continue
    maj="$(node_major "$cand")"
    [ -n "$maj" ] || continue
    if [ "$want" = exact ]; then
      [ "$maj" = "$REQ_MAJOR" ] && { printf '%s\n' "$cand"; return 0; }
    else
      [ "$maj" -gt "$REQ_MAJOR" ] && { printf '%s\n' "$cand"; return 0; }
    fi
  done < <(node_candidates)
  return 1
}

if [ -z "${NODE_BIN:-}" ]; then
  NODE_BIN="$(pick_node exact || pick_node newer || true)"
fi

if [ -z "${NODE_BIN:-}" ] || [ ! -x "${NODE_BIN:-}" ]; then
  echo "找不到 Node ${REQ_MAJOR}（当前 PATH 上的是 $(node -v 2>/dev/null || echo '无')）。" >&2
  echo "请安装 Node ${REQ_MAJOR}（nvm install ${REQ_MAJOR}），或显式指定：NODE_BIN=/path/to/node $0" >&2
  exit 1
fi

ACTUAL_MAJOR="$(node_major "$NODE_BIN")"
if [ -z "$ACTUAL_MAJOR" ]; then
  echo "无法识别 $NODE_BIN 的版本。" >&2
  exit 1
fi
# 硬校验：低于 .nvmrc 要求的大版本直接拒绝构建。
# 有了这一条，「CI 里构建成功」本身就证明 CI 用的是够新的 Node，不必去翻日志。
# 显式传 NODE_BIN 时同样会校验，避免用错版本编出与 CI 不一致的产物。
if [ "$ACTUAL_MAJOR" -lt "$REQ_MAJOR" ]; then
  echo "Node 大版本过低：.nvmrc 要求 ${REQ_MAJOR}，$NODE_BIN 是 ${ACTUAL_MAJOR}。" >&2
  exit 1
fi

# 让 npm / npx 与 NODE_BIN 同源，避免「node 来自一处、npm 却来自另一处」。
export PATH="$(dirname "$NODE_BIN"):$PATH"

# ---------- Go（版本要求见 server/go.mod 的 go 指令）----------
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

# bash 3.2（macOS 自带）会把 $VAR 后紧跟的全角字符首字节吞进变量名，
# 报 "unbound variable"——变量一律用 ${VAR} 显式闭合。
say "node $("$NODE_BIN" -v)（${NODE_BIN}）"
say "go   $("$GO_BIN" version | awk '{print $3}')（${GO_BIN}）"

# 「.nvmrc 写 26、本机只装了 28」这类情况：能跑，但要让用的人知道自己用的不是声明的大版本。
if [ "$ACTUAL_MAJOR" != "$REQ_MAJOR" ]; then
  printf '  ⚠ .nvmrc 要求的是 Node %s，上面用的是 %s（本机没有 %s 大版本，已退让到更高的版本）\n' \
    "$REQ_MAJOR" "$ACTUAL_MAJOR" "$REQ_MAJOR" >&2
fi

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
