#!/usr/bin/env bash
# The agent eval in docs/EVAL.md: each task in tasks.jsonl, with and without weave,
# in a fresh clone of the repository, one transcript per run.
#
#   bench/eval/run.sh <repo> <out-dir> [runs] [model]
#   TASKS=bench/eval/tasks-documenso.jsonl bench/eval/run.sh bench/repos/documenso …
#   NO_INIT=1 …   the weave arm without the CLAUDE.md section init writes (the first pilot)
#
# Needs the `claude` CLI signed in, `weave` on PATH (or WEAVE=/path), jq and git.
# Every run costs real tokens: runs=1 over the four tasks is eight sessions.
# The agent gets file tools and a short list of commands, never a blanket bypass.
set -euo pipefail
repo=$(cd "$1" && pwd)
mkdir -p "$2"; out=$(cd "$2" && pwd)
runs=${3:-1}; model=${4:-sonnet}
weave=${WEAVE:-weave}; weave=$(command -v "$weave" || echo "$weave")
here=$(cd "$(dirname "$0")" && pwd)
max_turns=${MAX_TURNS:-40}
tasks=$(realpath "${TASKS:-$here/tasks.jsonl}")
tools='Read,Edit,Write,Glob,Grep,Bash(git diff:*),Bash(git status:*),Bash(ls:*),Bash(npx jest:*),Bash(npm test:*)'

while IFS= read -r task; do
  id=$(jq -r .id <<<"$task"); prompt=$(jq -r .prompt <<<"$task")
  for arm in baseline weave; do
    for n in $(seq 1 "$runs"); do
      name="$id.$arm.$n"
      if [ -s "$out/$name.jsonl" ]; then echo "skip $name"; continue; fi
      work=$(mktemp -d); git clone --quiet "$repo" "$work/repo"; cd "$work/repo"
      allowed=$tools
      if [ "$arm" = weave ]; then
        "$weave" index --root . >/dev/null
        # what `weave init` installs is part of the arm: the CLAUDE.md section that
        # says when to use which tool. Committed, so it is not counted as a change.
        if [ -z "${NO_INIT:-}" ]; then
          "$weave" init --only claude --root . >/dev/null
          git add -A && git -c user.name=eval -c user.email=eval@localhost commit --quiet -m "weave init"
        fi
        printf '{"mcpServers":{"weave":{"command":"%s","args":["mcp","--root","."]}}}' "$weave" > "$work/mcp.json"
        allowed="$allowed,mcp__weave"
      else
        echo '{"mcpServers":{}}' > "$work/mcp.json"
      fi
      echo "run $name"
      start=$(date +%s)
      claude -p "$prompt" --model "$model" --max-turns "$max_turns" \
        --output-format stream-json --verbose \
        --strict-mcp-config --mcp-config "$work/mcp.json" \
        --allowedTools "$allowed" > "$out/$name.jsonl" 2> "$out/$name.err" || true
      secs=$(( $(date +%s) - start ))
      # the outcome is read from the tree, not from the agent's summary
      set +e; "$weave" check --root . > "$out/$name.check" 2>&1; check=$?; set -e
      git status --porcelain | awk '{print $2}' > "$out/$name.files"
      git diff > "$out/$name.diff"
      jq -nc --arg id "$id" --arg arm "$arm" --argjson n "$n" --argjson secs "$secs" --argjson check "$check" \
        '{id:$id,arm:$arm,run:$n,secs:$secs,check:$check}' > "$out/$name.meta"
      cd "$here"; rm -rf "$work"
    done
  done
done < "$tasks"
cp "$tasks" "$out/tasks.jsonl"
echo "transcripts in $out; summarise with: node $here/report.mjs $out"
