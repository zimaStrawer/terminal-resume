.PHONY: build local serve test vet preview

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
