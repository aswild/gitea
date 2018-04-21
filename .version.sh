#!/bin/sh -e

codeversion=`sed -n 's/^var Version.*"\([^"]\+\).*/\1/p' main.go`
gitversion=`git describe --tags --abbrev=0 | sed 's/v\([^-]\+\).*/\1/'`

if ( echo "$codeversion" | grep -q "$gitversion" ); then
    # on a release branch use the usual format
    git describe --tags --always --dirty=+ | sed 's/-/+/; s/^v//'
else
    # working off master, use branch name and rev count
    branch=`git symbolic-ref HEAD | sed -n 's:^refs/heads/::p'` || true
    sha=`git rev-parse --short HEAD`
    dirty=
    if [ -n "`git status --porcelain --untracked=no`" ]; then
        dirty="+"
    fi
    if [ -n "$branch" ]; then
        echo "${codeversion}-${branch}-g${sha}${dirty}"
    else
        echo "${codeversion}-g${sha}${dirty}"
    fi
fi
