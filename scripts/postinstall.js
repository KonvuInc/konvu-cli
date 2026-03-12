#!/usr/bin/env node
// Downloads the correct pre-built binary for the current platform.
const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const https = require("https");

const PLATFORM_MAP = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64",
};

const platform = PLATFORM_MAP[process.platform];
const arch = ARCH_MAP[process.arch];

if (!platform || !arch) {
  console.error(`Unsupported platform: ${process.platform}-${process.arch}`);
  process.exit(1);
}

const version = require("../package.json").version;
const ext = platform === "windows" ? "zip" : "tar.gz";
const filename = `konvu-${platform}-${arch}.${ext}`;
const url = `https://github.com/KonvuTeam/konvu-cli/releases/download/v${version}/${filename}`;

const binDir = path.join(__dirname, "..", "bin");
fs.mkdirSync(binDir, { recursive: true });

console.log(`Downloading konvu ${version} for ${platform}-${arch}...`);

const binPath = path.join(binDir, platform === "windows" ? "konvu.exe" : "konvu");

const file = fs.createWriteStream(path.join(binDir, filename));
https.get(url, (response) => {
  if (response.statusCode === 302) {
    https.get(response.headers.location, (r) => {
      r.pipe(file);
      file.on("finish", () => {
        file.close();
        extract(path.join(binDir, filename), binDir, platform);
      });
    });
  } else {
    response.pipe(file);
    file.on("finish", () => {
      file.close();
      extract(path.join(binDir, filename), binDir, platform);
    });
  }
});

function extract(archive, dest, platform) {
  if (platform === "windows") {
    execSync(`powershell -command "Expand-Archive -Path '${archive}' -DestinationPath '${dest}' -Force"`);
  } else {
    execSync(`tar -xzf "${archive}" -C "${dest}"`);
  }
  fs.unlinkSync(archive);
  if (platform !== "windows") {
    fs.chmodSync(binPath, 0o755);
  }
  console.log("konvu installed successfully.");
}
