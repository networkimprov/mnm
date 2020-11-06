#!/bin/bash
#  Copyright 2020 Liam Breck
#  Published at https://github.com/networkimprov/mnm-hammer
#
#  This Source Code Form is subject to the terms of the Mozilla Public
#  License, v. 2.0. If a copy of the MPL was not distributed with this
#  file, You can obtain one at http://mozilla.org/MPL/2.0/

set -e

bins=(
   'GOOS=linux   GOARCH=amd64'
   'GOOS=darwin  GOARCH=amd64'
#  'GOOS=windows GOARCH=amd64'
)

go build
app="$(basename "$PWD")"
appdir=mnm-tmtpd
files=("$app" LICENSE mnm.conf)
fileswin=("${files[@]}")
fileswin[0]+=.exe
ver=($("./$app" --version))
symln="$appdir-${ver[-2]}"

ln -s "$app" "../$symln" || test -L "../$symln"

for pf in "${bins[@]}"; do
   export $pf
   echo -n "--- ${ver[@]} $GOOS-$GOARCH: build"
   go build -a
   echo " & package ---"
   dst="$app/$appdir-$GOOS-$GOARCH-${ver[-2]}"
   if [ $GOOS = windows ]; then
      ls -sd "${fileswin[@]}"
      (cd ..; zip -rq "$dst.zip" "${fileswin[@]/#/$symln/}")
      rm "$app.exe"
   else
      ls -sd "${files[@]}"
      (cd ..; tar -czf "$dst.tgz" "${files[@]/#/$symln/}")
   fi
done

rm "../$symln"

GOOS='' GOARCH='' go build

