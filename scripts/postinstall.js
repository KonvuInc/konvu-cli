#!/usr/bin/env node
// Downloads the correct pre-built binary for the current platform
// and verifies its SHA256 checksum before extracting.
const { execSync } = require("child_process");
const crypto = require("crypto");
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
const baseURL = `https://github.com/KonvuTeam/konvu-cli/releases/download/v${version}`;
const archiveURL = `${baseURL}/${filename}`;
const checksumURL = `${baseURL}/checksums.txt`;

const binDir = path.join(__dirname, "..", "bin");
fs.mkdirSync(binDir, { recursive: true });

const binPath = path.join(
  binDir,
  platform === "windows" ? "konvu.exe" : "konvu"
);

console.log(`Downloading konvu ${version} for ${platform}-${arch}...`);

// Follow one redirect (GitHub releases always 302 to the CDN).
function download(url) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, { timeout: 60000 }, (response) => {
      if (response.statusCode === 302 || response.statusCode === 301) {
        download(response.headers.location).then(resolve, reject);
        return;
      }
      if (response.statusCode !== 200) {
        reject(new Error(`Download failed: HTTP ${response.statusCode}`));
        return;
      }
      const chunks = [];
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => resolve(Buffer.concat(chunks)));
      response.on("error", reject);
    });
    request.on("error", reject);
    request.on("timeout", () => {
      request.destroy();
      reject(new Error("Download timed out"));
    });
  });
}

function sha256(buffer) {
  return crypto.createHash("sha256").update(buffer).digest("hex");
}

function parseChecksums(text) {
  const map = {};
  for (const line of text.toString().split("\n")) {
    const parts = line.trim().split(/\s+/);
    if (parts.length === 2) {
      map[parts[1]] = parts[0];
    }
  }
  return map;
}

async function main() {
  // Download checksums and archive in parallel.
  const [checksumData, archiveData] = await Promise.all([
    download(checksumURL),
    download(archiveURL),
  ]);

  // Verify checksum.
  const checksums = parseChecksums(checksumData);
  const expected = checksums[filename];
  if (!expected) {
    console.error(
      `Checksum verification failed: ${filename} not found in checksums.txt`
    );
    process.exit(1);
  }

  const actual = sha256(archiveData);
  if (actual !== expected) {
    console.error("Checksum verification failed!");
    console.error(`  Expected: ${expected}`);
    console.error(`  Got:      ${actual}`);
    console.error(
      "The downloaded binary may have been tampered with. Aborting."
    );
    process.exit(1);
  }

  console.log("Checksum verified.");

  // Write archive, extract, and clean up.
  const archivePath = path.join(binDir, filename);
  fs.writeFileSync(archivePath, archiveData);

  if (platform === "windows") {
    execSync(
      `powershell -command "Expand-Archive -Path '${archivePath}' -DestinationPath '${binDir}' -Force"`
    );
  } else {
    execSync(`tar -xzf "${archivePath}" -C "${binDir}"`);
  }

  fs.unlinkSync(archivePath);

  if (platform !== "windows") {
    fs.chmodSync(binPath, 0o755);
  }

  console.log("konvu installed successfully.");
}

main().catch((err) => {
  console.error(`Installation failed: ${err.message}`);
  process.exit(1);
});
