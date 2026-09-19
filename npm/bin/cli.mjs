#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const supported = new Set([
  "darwin-arm64",
  "darwin-amd64",
  "linux-arm64",
  "linux-amd64",
  "win32-arm64",
  "win32-amd64",
]);
const target = `${process.platform}-${process.arch}`;

if (!supported.has(target)) {
  console.error(`暂不支持 ${target}。当前支持 macOS、Linux、Windows 的 ARM64 / x64。`);
  process.exit(1);
}

if (!process.stdin.isTTY || !process.stdout.isTTY) {
  console.error("请在交互式终端中运行终端简历。此程序需要 TTY。");
  process.exit(1);
}

const suffix = process.platform === "win32" ? ".exe" : "";
const here = dirname(fileURLToPath(import.meta.url));
const executable = join(here, "native", `terminal-resume-${target}${suffix}`);
const result = spawnSync(executable, ["--local", ...process.argv.slice(2)], {
  stdio: "inherit",
  windowsHide: false,
});

if (result.error) {
  console.error(`终端简历启动失败：${result.error.message}`);
  process.exit(1);
}

if (result.signal) {
  process.exitCode = 1;
} else {
  process.exitCode = result.status ?? 1;
}
