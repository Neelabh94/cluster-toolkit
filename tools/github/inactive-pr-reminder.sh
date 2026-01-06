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
# Original values for day-based monitoring
DAYS_INTERVAL=15
DAYS_TO_CLOSE=600 # Will be changed to 180 once the current backlog is cleared

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
	pr_info=$(gh pr view "$pr_number" --json updatedAt,isDraft,comments,author,reviewDecision)
	updated_at=$(echo "$pr_info" | jq -r '.updatedAt')
	is_draft=$(echo "$pr_info" | jq '.isDraft')
	pr_author=$(echo "$pr_info" | jq -r '.author.login')

	if [[ "$is_draft" == "true" ]]; then
		echo "PR #${pr_number} is a draft, skipping."
		continue
	fi

	# Check for approval status from the "multi-approvers" workflow
	echo "Checking approval status for PR #${pr_number}..."
	has_approvals=false
	# The `gh pr checks` command does not always return JSON.
	# We capture its output and check if it's a JSON array before parsing.
	checks_output=$(gh pr checks "$pr_number" || true)
	if [[ -n "$checks_output" && "$checks_output" == \[* ]]; then
		checks_conclusion=$(echo "$checks_output" | jq -r '.[] | select(.name | contains("multi-approvers")) | .conclusion' || true)
		if [[ "$checks_conclusion" == "SUCCESS" ]]; then
			has_approvals=true
		fi
	fi

	if [[ "$has_approvals" == "true" ]]; then
		echo "PR #${pr_number} has sufficient approvals."
	else
		echo "PR #${pr_number} does not have sufficient approvals."
	fi

	# Check for requested changes
	echo "Checking for requested changes on PR #${pr_number}..."
	changes_requested=false
	review_decision=$(echo "$pr_info" | jq -r '.reviewDecision')
	if [[ "$review_decision" == "CHANGES_REQUESTED" ]]; then
		changes_requested=true
		echo "PR #${pr_number} has requested changes."
	else
		echo "PR #${pr_number} does not have requested changes."
	fi

	# Inactivity calculation
	updated_at_seconds=$(date -d "$updated_at" +%s)
	now_seconds=$(date +%s)
	inactive_seconds=$((now_seconds - updated_at_seconds))

	# Original inactivity calculation based on days
	inactive_days=$((inactive_seconds / 86400))
	inactive_unit="days"
	inactive_value=${inactive_days}
	interval=${DAYS_INTERVAL}
	close_threshold=${DAYS_TO_CLOSE}

	echo "PR #${pr_number} has been inactive for ${inactive_value} ${inactive_unit}."

	# Closing logic - only close if not approved
	if ((inactive_value > close_threshold)) && [[ "$has_approvals" == "false" ]]; then
		echo "PR #${pr_number} is unapproved and has been inactive for more than ${close_threshold} ${inactive_unit}. Closing."
		gh pr comment "$pr_number" --body "@${pr_author} This PR was automatically closed after being inactive for more than ${close_threshold} ${inactive_unit}. ${REMINDER_MARKER}"
		gh pr close "$pr_number"
		continue # Move to the next PR
	fi

	# Reminder logic
	comments=$(echo "$pr_info" | jq -r '.comments[].body')
	# grep returns 1 if no lines are selected, `|| true` prevents the script from exiting
	reminder_count=$(echo "$comments" | grep -c "$REMINDER_MARKER" || true)
	echo "Found ${reminder_count} reminder(s) for PR #${pr_number}."

	expected_reminders=$((inactive_value / interval))

	if ((expected_reminders > reminder_count)); then
		echo "Expected ${expected_reminders} reminder(s), found ${reminder_count}. Sending a new reminder."
		if [[ "$has_approvals" == "true" ]]; then
			gh pr comment "$pr_number" --body "This PR is approved and has been inactive for ${inactive_value} ${inactive_unit}. @${pr_author}, please merge it. ${REMINDER_MARKER}"
		else
			if [[ "$changes_requested" == "true" ]]; then
				gh pr comment "$pr_number" --body "This PR has been inactive for ${inactive_value} ${inactive_unit} and has changes requested. @${pr_author}, please address the requested changes or close the PR if it's no longer needed. ${REMINDER_MARKER}"
			else
				gh pr comment "$pr_number" --body "This PR has been inactive for ${inactive_value} ${inactive_unit} and has no unresolved comments. @GoogleCloudPlatform/hpc-toolkit, please review. ${REMINDER_MARKER}"
			fi
		fi
	else
		echo "No new reminder needed for PR #${pr_number}."
	fi
done
echo "---"
echo "All pull requests checked."
