#!/bin/bash
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

# This script finds pull requests that have been inactive for a certain
# period and posts reminders. It closes PRs that have been inactive for too long.
#
# Markers and thresholds
REMINDER_MARKER="<!-- PR_INACTIVITY_REMINDER -->"
DAYS_INTERVAL=30
DAYS_TO_CLOSE=180

echo "Fetching open pull requests..."
# The `gh pr list` command can fail if no PRs are open.
# We use `|| true` to prevent the script from exiting.
# The loop will simply not run if the output is empty.
pr_numbers=$(gh pr list --json number --jq '.[].number' || true)

if [ -z "$pr_numbers" ]; then
	echo "No open pull requests found."
	exit 0
fi

for pr_number in $pr_numbers; do
	echo "---"
	echo "Checking PR #${pr_number}"
	pr_info=$(gh pr view "$pr_number" --json updatedAt,isDraft,comments)
	updated_at=$(echo "$pr_info" | jq -r '.updatedAt')
	is_draft=$(echo "$pr_info" | jq '.isDraft')

	if [[ "$is_draft" == "true" ]]; then
		echo "PR #${pr_number} is a draft, skipping."
		continue
	fi

	# Inactivity calculation
	updated_at_seconds=$(date -d "$updated_at" +%s)
	now_seconds=$(date +%s)
	inactive_seconds=$((now_seconds - updated_at_seconds))
	inactive_days=$((inactive_seconds / 86400))

	echo "PR #${pr_number} has been inactive for ${inactive_days} days."

	# Closing logic
	if ((inactive_days > DAYS_TO_CLOSE)); then
		echo "PR #${pr_number} has been inactive for more than ${DAYS_TO_CLOSE} days. Closing."
		gh pr comment "$pr_number" --body "This PR was automatically closed after being inactive for more than ${DAYS_TO_CLOSE} days. ${REMINDER_MARKER}"
		gh pr close "$pr_number"
		continue # Move to the next PR
	fi

	# Reminder logic
	comments=$(echo "$pr_info" | jq -r '.comments[].body')
	# grep returns 1 if no lines are selected, `|| true` prevents the script from exiting
	reminder_count=$(echo "$comments" | grep -c "$REMINDER_MARKER" || true)
	echo "Found ${reminder_count} reminder(s) for PR #${pr_number}."

	expected_reminders=$((inactive_days / DAYS_INTERVAL))

	if ((expected_reminders > reminder_count)); then
		echo "Expected ${expected_reminders} reminder(s), found ${reminder_count}. Sending a new reminder."
		gh pr comment "$pr_number" --body "This PR has been inactive for ${inactive_days} days. Please update it or close it if it is no longer needed. ${REMINDER_MARKER}"
	else
		echo "No new reminder needed for PR #${pr_number}."
	fi
done
echo "---"
echo "All pull requests checked."
