#!/usr/bin/env bash
# Runs every example under examples/ and compares its output to its README.
# Maintainers run this script locally; the retained CI workflow uses it too. It extracts the `sh` command block
# and the final `text` block from each example's README, runs the command from
# the repository root, and fails if stdout does not match.
set -euo pipefail

cd "$(dirname "$0")/.."

fail=0

# The set of example folders (every dir under examples/ holding a README.md).
dirs=()
while IFS= read -r d; do
  dirs+=("$d")
done < <(find examples -mindepth 1 -maxdepth 1 -type d | sort)

# The index must list exactly the example folders, no more, no fewer.
for d in "${dirs[@]}"; do
  name="$(basename "$d")"
  if ! grep -q "($name/)" examples/README.md; then
    echo "FAIL: examples/README.md does not link example '$name'"
    fail=1
  fi
done
for linked in $(grep -oE '\(([a-z0-9-]+)/\)' examples/README.md | tr -d '()/'); do
  if [ ! -d "examples/$linked" ]; then
    echo "FAIL: examples/README.md links '$linked', which does not exist"
    fail=1
  fi
done

# extract_block FILE LANG N — print the Nth fenced block of the given language.
extract_block() {
  awk -v lang="$2" -v want="$3" '
    $0 == "```" lang { n++; if (n == want) { inblk = 1; next } }
    inblk && $0 == "```" { exit }
    inblk { print }
  ' "$1"
}

# extract_last_block FILE LANG — print the FINAL fenced block of the given
# language. Buffers each block and emits the last one at END, so a README may
# carry intro snippets before the block that holds the real expected output.
extract_last_block() {
  awk -v lang="$2" '
    $0 == "```" lang { inblk = 1; n = 0; next }
    inblk && $0 == "```" { inblk = 0; cnt = n; for (i = 1; i <= n; i++) out[i] = cur[i]; next }
    inblk { cur[++n] = $0 }
    END { for (i = 1; i <= cnt; i++) print out[i] }
  ' "$1"
}

# Root README blocks are compared as data, never evaluated. The fixed first task
# is executed later through the allowlisted custom-checker example command.
expect_root_block() {
  local block_number="$1" expected="$2" label="$3" actual
  actual="$(extract_block README.md sh "$block_number")"
  if [ "$actual" != "$expected" ]; then
    echo "FAIL: root README $label command drifted"
    fail=1
  fi
}

example_command="$(extract_block examples/custom-checker/README.md sh 1)"
if [ "$example_command" != 'go run ./examples/custom-checker' ]; then
  echo "FAIL: custom-checker command drifted from the fixed quickstart contract"
  fail=1
fi
expect_root_block 1 'git clone https://github.com/tetriz-ai/local-ci.git
cd local-ci
go run ./examples/custom-checker' 'offline quickstart'
root_output="$(extract_block README.md text 1)"
example_output="$(extract_last_block examples/custom-checker/README.md text)"
if [ -z "$root_output" ] || [ "$root_output" != "$example_output" ]; then
  echo "FAIL: root README quickstart output differs from the custom-checker example"
  fail=1
fi

# Named execution exclusions: downloads/source installs need published network
# artifacts; live PR runs mutate GitHub; deps usage requires the current checkout.
# Their exact commands remain checked for drift instead of being executed here.
expect_root_block 2 'mkdir -p /tmp/local-ci-v0.1.0
cd /tmp/local-ci-v0.1.0
curl -fLO https://github.com/tetriz-ai/local-ci/releases/download/v0.1.0/local-ci_0.1.0_darwin_arm64.tar.gz
curl -fLO https://github.com/tetriz-ai/local-ci/releases/download/v0.1.0/checksums.txt
shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf local-ci_0.1.0_darwin_arm64.tar.gz
./local-ci --version
./deps --help' 'binary installation (execution excluded: published network artifacts)'
expect_root_block 3 'go install github.com/tetriz-ai/local-ci/cmd/local-ci@v0.1.0
go install github.com/tetriz-ai/local-ci/cmd/deps@v0.1.0' 'source installation (execution excluded: published network modules)'
expect_root_block 4 'local-ci --pr 1234' 'live PR quickstart (execution excluded: GitHub mutation)'
expect_root_block 5 '# from the repository root, with gh authenticated
local-ci --pr 1234' 'live PR usage (execution excluded: GitHub mutation)'
expect_root_block 6 'go install github.com/tetriz-ai/local-ci/cmd/deps@v0.1.0
deps impacteddirs --modified_dirs="projectinfo"' 'deps usage (execution excluded: install and checkout-dependent result)'
root_blocks="$(awk '$0 == "```sh" { n++ } END { print n+0 }' README.md)"
if [ "$root_blocks" -ne 6 ]; then
  echo "FAIL: root README shell block count changed; update the fixed drift contract"
  fail=1
fi
if [ "$fail" -ne 0 ]; then
  echo "examples: FAILED"
  exit 1
fi
if [ "${1:-}" = '--docs-only' ]; then
  echo 'examples: README drift checks passed (execution excluded in --docs-only mode)'
  exit 0
fi
if [ "$#" -ne 0 ]; then
  echo 'usage: scripts/run-examples.sh [--docs-only]' >&2
  exit 1
fi

# Examples must at least vet.
go vet ./examples/...

for d in "${dirs[@]}"; do
  name="$(basename "$d")"
  readme="$d/README.md"
  cmd="$(extract_block "$readme" sh 1)"
  # The expected output is the README's FINAL text block, so intro snippets before
  # it are ignored. (For today's single-block examples the last block is the only one.)
  want="$(extract_last_block "$readme" text)"
  echo "== $name: $cmd"
  if [ "$cmd" != "go run ./$d" ]; then
    echo "FAIL: $readme must use its fixed go run command"
    fail=1
    continue
  fi
  got="$(go run "./$d" 2>/dev/null)"
  if [ "$got" != "$want" ]; then
    echo "FAIL: $name output did not match $readme"
    diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") || true
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "examples: FAILED"
  exit 1
fi
echo "examples: all passed"
