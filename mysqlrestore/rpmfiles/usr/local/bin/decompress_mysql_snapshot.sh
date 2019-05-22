#!/usr/bin/env bash

set -ue
set -o pipefail

find "${1}" -name '*.qp' -type f -exec du --null -h '{}' + | sort -rhz | sed -z 's/[[:digit:]\.]\+[A-Za-z]\+[[:space:]]\+//' | parallel -j 50% --null 'qpress -do {} > {.} && rm -f {}'