#!/bin/sh
# Shows, before a publish, how the text the agent receives has changed since
# the last release, and asks a person to confirm it reads right.
#
# Why: 0.19.0 shipped the Message-style text pasted above the user's words, so
# the agent answered the style instead of the user. Every test passed (they
# asserted that layout); a person reading the agent's text would have seen it.
#
# Set AGENT_SEES_OK=yes to answer the question up front (non-interactive runs).
set -eu
cd "$(dirname "$0")/.."
file=testdata/agent-sees.txt

# 1. The saved sample must match what this build really sends.
if ! go test -count=1 -run '^TestAgentSees$' . >/dev/null 2>&1; then
	echo "STOP: $file does not match what the agent receives."
	echo "Run 'make unit-test' to see the new text (TestAgentSees prints it); if it is right: make agent-sees, then commit $file."
	exit 1
fi

# 2. Compare with the last release. `make bump` + its commit run before
#    publish, so skip HEAD when HEAD is itself the release commit.
last=$(git log --grep='^chore(release):' -1 --format=%h HEAD~1)
label=$(git log -1 --format=%s "$last" | sed 's/^chore(release): //')
old=$(mktemp)
trap 'rm -f "$old"' EXIT
git show "$last:$file" >"$old" 2>/dev/null || : >"$old"

if cmp -s "$old" "$file"; then
	echo "What the agent receives: unchanged since $label ($last)."
	exit 0
fi

echo "================================================================"
echo "What the agent receives CHANGED since $label ($last)."
echo "Lines starting with - are the old text, + the new."
echo "================================================================"
diff -u --label "$label" --label "this release" "$old" "$file" || true
echo
echo "================================================================"
echo "Read it as the agent would: is the user's message clearly separate"
echo "from the style, attachments and instructions around it?"
echo "================================================================"

if [ "${AGENT_SEES_OK:-}" = "yes" ]; then
	echo "AGENT_SEES_OK=yes: confirmed up front."
	exit 0
fi
if [ ! -t 0 ]; then
	echo "STOP: no terminal to ask on. Re-run with AGENT_SEES_OK=yes once a person has read the above."
	exit 1
fi
printf 'Type yes to continue publishing: '
read -r ans
[ "$ans" = "yes" ] || { echo "Not publishing."; exit 1; }
