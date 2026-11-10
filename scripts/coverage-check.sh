#!/usr/bin/env bash
# Enforces per-layer coverage thresholds on a Go coverage profile.
#
# Usage: scripts/coverage-check.sh [coverage.out]
#
# Thresholds (per package):
#   internal/domain/...          90%
#   internal/application/...     90%
#   internal/adapters/http/...   80%
#   internal/adapters/grpc/...   80%
#   overall                      80%
#
# Excluded from this unit-test gate:
#   cmd/...                               composition root, exercised by integration/E2E runs
#   gen/...                               generated protobuf code
#   internal/infrastructure/database      needs a real MongoDB, see `make test-integration`
#   internal/adapters/repository/mongodb  needs a real MongoDB, see `make test-integration`
set -euo pipefail

cd "$(dirname "$0")/.."

profile="${1:-coverage.out}"

if [[ ! -f "$profile" ]]; then
	echo "coverage-check: profile '$profile' not found; run 'make test-coverage' first" >&2
	exit 1
fi

module="$(awk '$1 == "module" { print $2; exit }' go.mod)"

awk -v module="$module" -v overall_min=80 '
# min_coverage returns the threshold for a package path relative to the module
# root, or -1 when the package only counts toward the overall total.
function min_coverage(pkg) {
	if (pkg ~ /^internal\/domain(\/|$)/) return 90
	if (pkg ~ /^internal\/application(\/|$)/) return 90
	if (pkg ~ /^internal\/adapters\/http(\/|$)/) return 80
	if (pkg ~ /^internal\/adapters\/grpc(\/|$)/) return 80
	return -1
}

function excluded(pkg) {
	return pkg ~ /^(cmd|gen)(\/|$)/ ||
		pkg ~ /^internal\/infrastructure\/database(\/|$)/ ||
		pkg ~ /^internal\/adapters\/repository\/mongodb(\/|$)/
}

function row(pkg, coverage, min, result) {
	printf "%-48s %9s %5s  %s\n", pkg, coverage, min, result
}

# Each line: <import path>/<file>.go:<start>,<end> <statements> <hit count>
$1 ~ /^mode:/ { next }
NF == 3 {
	block = $1
	pkg = block
	sub(/:.*$/, "", pkg)      # drop the block position
	sub(/\/[^\/]*$/, "", pkg) # drop the file name
	if (index(pkg, module "/") == 1) pkg = substr(pkg, length(module) + 2)
	else if (pkg == module) pkg = "."

	# A block can repeat when profiles are merged; count its statements once.
	package_of[block] = pkg
	statements[block] = $2
	if ($3 > 0) hit[block] = 1
}

END {
	n = 0
	for (block in statements) {
		pkg = package_of[block]
		if (!(pkg in total)) {
			names[++n] = pkg
			total[pkg] = 0
			covered[pkg] = 0
		}
		total[pkg] += statements[block]
		if (block in hit) covered[pkg] += statements[block]
	}

	# Insertion sort keeps the report in a stable, readable order.
	for (i = 2; i <= n; i++) {
		name = names[i]
		for (j = i - 1; j > 0 && names[j] > name; j--) names[j + 1] = names[j]
		names[j + 1] = name
	}

	row("PACKAGE", "COVERAGE", "MIN", "RESULT")
	failed = 0
	for (i = 1; i <= n; i++) {
		pkg = names[i]
		if (excluded(pkg)) {
			row(pkg, "-", "-", "EXCLUDED")
			continue
		}
		overall_total += total[pkg]
		overall_covered += covered[pkg]
		if (total[pkg] == 0) {
			row(pkg, "n/a", "-", "NO STATEMENTS")
			continue
		}
		pct = 100 * covered[pkg] / total[pkg]
		min = min_coverage(pkg)
		if (min < 0) {
			row(pkg, sprintf("%.1f%%", pct), "-", "OK")
		} else if (pct < min) {
			row(pkg, sprintf("%.1f%%", pct), min "%", "FAIL")
			failed = 1
		} else {
			row(pkg, sprintf("%.1f%%", pct), min "%", "OK")
		}
	}

	if (overall_total == 0) {
		print "coverage-check: no statements to measure"
		exit 1
	}
	pct = 100 * overall_covered / overall_total
	result = "OK"
	if (pct < overall_min) {
		result = "FAIL"
		failed = 1
	}
	row("OVERALL", sprintf("%.1f%%", pct), overall_min "%", result)
	exit failed
}
' "$profile"
