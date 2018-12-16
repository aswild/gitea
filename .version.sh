#!/bin/sh

set -e

codeversion=`sed -n 's/^var Version.*"\([^"]\+\).*/\1/p' main.go`
gitversion=`git describe --tags --abbrev=0 | sed 's/v\([^-]\+\).*/\1/'`
branch=`git symbolic-ref HEAD 2>/dev/null | sed -n 's:^refs/heads/::p'` || true

if ! (echo "$branch" | grep master >/dev/null 2>&1); then
    # on a release branch use the usual format
    tag="$(git describe --tags --always --abbrev=0)"
    git describe --tags --always --dirty=+ | sed "s/${tag}-/${tag}+/; s/^v//"
else
    # working off master, use branch name and rev count
    [ -z "$branch" ] || branch="-$branch"
    sha=`git rev-parse --short HEAD`
    revcount=`git rev-list HEAD | wc -l`
    dirty=
    if [ -n "`git status --porcelain --untracked=no`" ]; then
        dirty="+"
    fi
    echo "${codeversion}${branch}-${revcount}-g${sha}${dirty}"
fi
