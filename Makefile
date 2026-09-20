.PHONY: build local serve test vet preview sync

BINARY := terminal-resume
SOURCES := $(wildcard cmd/terminal-resume/*.go internal/app/*.go profile/*.go profile/*.yaml)

# 预览用的运行时：默认取 PATH 里的 python3 / node；
# 装了 @xterm/headless 的目录通过 PREVIEW_NODE_PATH 指过去（默认 npm 全局目录）。
PREVIEW_PYTHON ?= python3
PREVIEW_NODE ?= node
PREVIEW_NODE_PATH ?= $(shell npm root -g 2>/dev/null)

build: $(BINARY)

$(BINARY): $(SOURCES) go.mod go.sum Makefile
	go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/terminal-resume

local: $(BINARY)
	./$(BINARY) --local

serve: $(BINARY)
	./$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

# 重新生成 docs/tui-preview.html：真实跑一遍二进制 -> pty 攒字节流 -> xterm.js 还原。
preview: $(BINARY)
	$(PREVIEW_PYTHON) scripts/capture-tui-frames.py
	NODE_PATH=$(PREVIEW_NODE_PATH) $(PREVIEW_NODE) scripts/render-tui-preview.js

# 重编 6 平台二进制并同步到线上托管源（Zima_room2.0/public）。
# 二进制有两个落点，只更新一处的话线上会一直跑旧版本 —— 用这个目标代替手工编译。
# 目标目录可用 PORTFOLIO_PUBLIC 覆盖；同步完仍需在作品集站仓库 commit + push 才会上线。
sync:
	bash scripts/sync-binaries.sh
