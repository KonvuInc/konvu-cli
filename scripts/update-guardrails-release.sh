#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "usage: $0 <version> | $0 --check" >&2
  exit 2
}

mode=write
if [[ ${1:-} == "--check" ]]; then
  mode=check
  shift
fi
destination=cmd/guardrails_artifacts.go
if [[ "$mode" == check && $# -eq 0 ]]; then
  version=$(awk -F\" '/^const guardrailsPinnedVersion = / { print $2; exit }' "$destination")
elif [[ "$mode" == write && $# -eq 1 ]]; then
  version=$1
else
  usage
fi
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "invalid Guardrails version: $version" >&2
  exit 1
fi

for command in cosign curl gofmt jq tar; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required" >&2
    exit 1
  fi
done

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

sha256_stdin() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum | awk '{ print $1 }'
  else
    shasum -a 256 | awk '{ print $1 }'
  fi
}

base_url="https://dneaqnz3vqe4a.cloudfront.net/guardrails/$version"
identity="https://github.com/KonvuTeam/konvu-guardrails/.github/workflows/release-guardrails.yml@refs/tags/$version"
issuer="https://token.actions.githubusercontent.com"
tmp_dir=$(mktemp -d)
trap 'rm -rf -- "$tmp_dir"' EXIT
manifest="$tmp_dir/guardrails-release.json"

# v0.5.1 predates the signed aggregate manifest, but every archive in that release has
# an equivalent keyless Sigstore bundle. Keep that one-release fallback so this script can
# reproduce the current trust anchor; all later releases must provide the manifest.
if curl --proto '=https' --tlsv1.2 -fsSLo "$manifest" \
  "$base_url/guardrails-release.json" 2>/dev/null; then
  curl --proto '=https' --tlsv1.2 -fsSLo \
    "$tmp_dir/guardrails-release.json.bundle" \
    "$base_url/guardrails-release.json.bundle"
  cosign verify-blob \
    --bundle "$tmp_dir/guardrails-release.json.bundle" \
    --certificate-identity "$identity" \
    --certificate-oidc-issuer "$issuer" \
    "$manifest" >/dev/null
else
  if [[ "$version" != "v0.5.1" ]]; then
    echo "signed Guardrails release manifest not found for $version" >&2
    exit 1
  fi

  artifacts='{}'
  for target in \
    aarch64-apple-darwin \
    x86_64-apple-darwin \
    aarch64-unknown-linux-gnu \
    x86_64-unknown-linux-gnu; do
    archive_name="guardrails-cli-$target.tar.xz"
    archive="$tmp_dir/$archive_name"
    bundle="$archive.bundle"
    curl --proto '=https' --tlsv1.2 -fsSLo "$archive" "$base_url/$archive_name"
    curl --proto '=https' --tlsv1.2 -fsSLo "$bundle" "$base_url/$archive_name.bundle"
    cosign verify-blob \
      --bundle "$bundle" \
      --certificate-identity "$identity" \
      --certificate-oidc-issuer "$issuer" \
      "$archive" >/dev/null

    guardrails_member=$(tar -tf "$archive" | awk -F/ '$NF == "guardrails" { print; exit }')
    scanner_member=$(tar -tf "$archive" | awk -F/ '$NF == "guardrails-resource-scan" { print; exit }')
    if [[ -z "$guardrails_member" || -z "$scanner_member" ]]; then
      echo "$archive_name is missing a Guardrails runtime binary" >&2
      exit 1
    fi

    archive_sha256=$(sha256_file "$archive")
    guardrails_sha256=$(tar -xOf "$archive" "$guardrails_member" | sha256_stdin)
    scanner_sha256=$(tar -xOf "$archive" "$scanner_member" | sha256_stdin)
    artifacts=$(jq -cS \
      --arg target "$target" \
      --arg archive_name "$archive_name" \
      --arg archive_sha256 "$archive_sha256" \
      --arg guardrails_sha256 "$guardrails_sha256" \
      --arg scanner_sha256 "$scanner_sha256" \
      '. + {($target): {
        archive: {name: $archive_name, sha256: $archive_sha256},
        binaries: {
          guardrails: {sha256: $guardrails_sha256},
          "guardrails-resource-scan": {sha256: $scanner_sha256}
        }
      }}' <<<"$artifacts")
  done
  jq -nS --arg version "$version" --argjson artifacts "$artifacts" \
    '{schema_version: 1, version: $version, artifacts: $artifacts}' > "$manifest"
fi

expected_repository="KonvuTeam/konvu-guardrails"
expected_workflow=".github/workflows/release-guardrails.yml"
if ! jq -e \
  --arg version "$version" \
  --arg repository "$expected_repository" \
  --arg workflow "$expected_workflow" \
  '. as $manifest |
   (.schema_version == 1) and (.version == $version) and
   ((($version == "v0.5.1") and (.source == null)) or
    ((.source.repository == $repository) and (.source.workflow == $workflow) and
     (.source.commit | test("^[0-9a-f]{40}$")))) and
   (["aarch64-apple-darwin", "x86_64-apple-darwin",
     "aarch64-unknown-linux-gnu", "x86_64-unknown-linux-gnu"] | all(
       . as $target |
       ($manifest.artifacts[$target].archive.name == ("guardrails-cli-" + $target + ".tar.xz")) and
       ($manifest.artifacts[$target].archive.sha256 | test("^[0-9a-f]{64}$")) and
       ($manifest.artifacts[$target].binaries.guardrails.sha256 | test("^[0-9a-f]{64}$")) and
       ($manifest.artifacts[$target].binaries["guardrails-resource-scan"].sha256 | test("^[0-9a-f]{64}$"))
     ))' "$manifest" >/dev/null; then
  echo "invalid Guardrails release manifest for $version" >&2
  exit 1
fi

generated="$tmp_dir/guardrails_artifacts.go"
jq -r \
  --arg version "$version" \
  'def artifact($target):
     "\t" + ($target | @json) + ": {\n" +
     "\t\tarchiveSHA256:         " + (.artifacts[$target].archive.sha256 | @json) + ",\n" +
     "\t\tmainSHA256:            " + (.artifacts[$target].binaries.guardrails.sha256 | @json) + ",\n" +
     "\t\tresourceScannerSHA256: " + (.artifacts[$target].binaries["guardrails-resource-scan"].sha256 | @json) + ",\n" +
     "\t},";
   "// Code generated by scripts/update-guardrails-release.sh; DO NOT EDIT.\n\n" +
   "package cmd\n\n" +
   "// guardrailsPinnedVersion is the Guardrails release installed by this version\n" +
   "// of Konvu CLI.\n" +
   "const guardrailsPinnedVersion = " + ($version | @json) + "\n\n" +
   "type guardrailsArtifact struct {\n" +
   "\tarchiveSHA256         string\n" +
   "\tmainSHA256            string\n" +
   "\tresourceScannerSHA256 string\n" +
   "}\n\n" +
   "// guardrailsArtifacts is the trust anchor for downloaded runtime bytes.\n" +
   "var guardrailsArtifacts = map[string]guardrailsArtifact{\n" +
   artifact("aarch64-apple-darwin") + "\n" +
   artifact("x86_64-apple-darwin") + "\n" +
   artifact("aarch64-unknown-linux-gnu") + "\n" +
   artifact("x86_64-unknown-linux-gnu") + "\n" +
   "}\n"' "$manifest" > "$generated"
gofmt -w "$generated"

if [[ "$mode" == check ]]; then
  if ! cmp -s "$generated" "$destination"; then
    echo "$destination does not match the signed Guardrails $version release" >&2
    exit 1
  fi
else
  cp "$generated" "$destination"
fi
