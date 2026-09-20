#!/bin/sh
set -eu
for file in internal/app/web/*.js; do node --check "$file"; done
for test in time-layout slot-preview navigation assistant-review windows-list booking-feedback repository-profile calendar-overlaps; do
  node "tests/browser/$test.cjs"
done
