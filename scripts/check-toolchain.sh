#!/usr/bin/env bash
# 工具链版本一致性检查。
#
# 「谁是准」是明确的：
#   Node 大版本 → .nvmrc
#   Go   版本   → server/go.mod 的 go 指令
# CI 与本地测试直接从这两个文件读（node-version-file / go-version-file），所以天然不会分叉。
#
# 唯一可能分叉的是 Dockerfile 里的基础镜像 tag —— 构建镜像那一步没法读这些文件，只能写死。
# 这个脚本就守这一处：dependabot 单独把 node:26-alpine 提到 node:28-alpine 时，
# CI 会在这里失败，并指出还要一起改哪几处。
# 大版本刻意不冻结（见 .github/dependabot.yml），所以靠校验而不是靠冻结来防分叉。
#
# 用法：./scripts/check-toolchain.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
problems=0

line() { printf '  %-26s %s\n' "$1" "$2"; }
bad() { echo "  ✗ $1"; problems=$((problems + 1)); }

# ---------- Node ----------
nvmrc="$(sed -n 's/^v\{0,1\}\([0-9][0-9]*\).*/\1/p' "$ROOT/.nvmrc" | head -1)"
img_node="$(sed -n 's/^FROM node:\([0-9][0-9]*\).*/\1/p' "$ROOT/Dockerfile" | head -1)"
eng_node="$(sed -n 's/.*"node"[[:space:]]*:[[:space:]]*">=\([0-9][0-9]*\)".*/\1/p' "$ROOT/web/package.json" | head -1)"

echo "Node"
line ".nvmrc（准）" "${nvmrc:-读不到}"
line "Dockerfile  FROM node:" "${img_node:-读不到}"
line "package.json  engines.node" ">=${eng_node:-读不到}"

if [ -z "$nvmrc" ]; then
  bad "从 .nvmrc 读不到大版本"
fi
if [ -n "$nvmrc" ] && [ "$img_node" != "$nvmrc" ]; then
  bad "Dockerfile 的 node 大版本是 ${img_node:-（读不到）}，.nvmrc 是 $nvmrc"
  echo "     一起改：Dockerfile、.nvmrc、web/package.json 的 engines（改完跑一次 npm install 同步 lockfile）"
fi
if [ -n "$nvmrc" ] && [ -n "$eng_node" ] && [ "$eng_node" -gt "$nvmrc" ]; then
  bad "engines.node 的下限（>=$eng_node）高于 .nvmrc（$nvmrc）：声明的大版本自己就装不上"
fi

# ---------- Go ----------
echo
echo "Go"

go_mod="$(sed -n 's/^go \([0-9][0-9.]*\).*/\1/p' "$ROOT/server/go.mod" | head -1)"
img_go="$(sed -n 's/^FROM golang:\([0-9][0-9.]*\).*/\1/p' "$ROOT/Dockerfile" | head -1)"
go_mm="$(printf '%s' "$go_mod" | cut -d. -f1,2)"
img_go_mm="$(printf '%s' "$img_go" | cut -d. -f1,2)"

line "server/go.mod（准）" "go ${go_mod:-读不到}"
line "Dockerfile  FROM golang:" "${img_go:-读不到}"

if [ -z "$go_mod" ]; then
  bad "从 server/go.mod 读不到 go 指令"
fi
if [ -n "$go_mod" ] && [ -n "$img_go" ] && [ "$img_go_mm" != "$go_mm" ]; then
  bad "Dockerfile 的 golang 是 $img_go，go.mod 的 go 指令是 $go_mod（按 major.minor 比）"
  echo "     一起改：Dockerfile 与 server/go.mod"
fi

echo
if [ "$problems" -ne 0 ]; then
  echo "工具链声明不一致：$problems 处。"
  exit 1
fi
echo "工具链声明一致 ✓"
