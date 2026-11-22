#!/bin/bash
# Greps the whole tree and the whole history for the words that name artificial intelligence tools, and prints each count; it exits 1 if any count
# is not zero. A domain or an account name that merely ends in ".ai" is not the word and is skipped. The words are put
# together from pieces so this file does not contain them itself.
cd "$(dirname "$0")/.." || exit 2
A=a; I=i
w1="${A}${I}"; w2="cla""ude"; w3="anthr${A}pic"; w4="l""lm"; w5="ass${I}stant"; w6="mo""dels?"; w7="gener${A}ted"; w8="co-auth""ored"; w9="open${w1}"; w10="chat""gpt"; w11="cop${I}lot"
PAT="(^|[^A-Za-z0-9.@_-])${w1}([^A-Za-z0-9._@-]|\$)|${w2}|${w3}|\\b${w4}\\b|${w5}|\\b${w6}\\b|${w7}|${w8}|${w9}|${w10}|${w11}|gpt-[0-9]"
EX=(':!go.sum' ':!ui/web/i18n/*.json' ':!ui/web/i18n/src/*')
bad=0
echo "tree:"
n=$(git grep -n -I -i -E "$PAT" -- . "${EX[@]}" | wc -l); echo "  $n"; [ "$n" -ne 0 ] && { bad=1; git grep -n -I -i -E "$PAT" -- . "${EX[@]}" | head -20; }
echo "history (every commit, every file):"
n=$(git grep -n -I -i -E "$PAT" $(git for-each-ref --format='%(refname)' refs/heads refs/tags | xargs -r git rev-list) -- . "${EX[@]}" 2>/dev/null | wc -l); echo "  $n"; [ "$n" -ne 0 ] && bad=1
echo "commit messages and authors:"
n=$(git log --branches --tags --format='%an %ae %cn %ce %s %b' | grep -i -E -c "$PAT"); echo "  $n"; [ "$n" -ne 0 ] && bad=1
exit $bad
