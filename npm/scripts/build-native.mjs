import { spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const projectRoot = resolve(packageRoot, "..");
const outputRoot = join(packageRoot, "bin", "native");
const targets = [
  ["darwin", "arm64"],
  ["darwin", "amd64"],
  ["linux", "arm64"],
  ["linux", "amd64"],
  ["windows", "arm64"],
  ["windows", "amd64"],
];

mkdirSync(outputRoot, { recursive: true });

for (const [goos, goarch] of targets) {
  const platform = goos === "windows" ? "win32" : goos;
  const suffix = goos === "windows" ? ".exe" : "";
  const output = join(outputRoot, `terminal-resume-${platform}-${goarch}${suffix}`);
  console.log(`Building ${platform}-${goarch}…`);
  const result = spawnSync(
    "go",
    ["build", "-trimpath", "-ldflags=-s -w", "-o", output, "./cmd/terminal-resume"],
    {
      cwd: projectRoot,
      stdio: "inherit",
      env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" },
    },
  );
  if (result.error) {
    console.error(`构建 ${platform}-${goarch} 失败：${result.error.message}`);
    process.exit(1);
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
