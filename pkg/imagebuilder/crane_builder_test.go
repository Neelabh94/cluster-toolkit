// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package imagebuilder

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/patternmatcher"
)

// Wrapper to simulate logic in processTarEntry
func testShouldIgnore(t *testing.T, matcher *patternmatcher.PatternMatcher, relPath string, isDir bool) bool {
	relPathSlash := filepath.ToSlash(relPath)
	if isDir && !strings.HasSuffix(relPathSlash, "/") {
		relPathSlash += "/"
	}
	// MatchesOrParentMatches is what we use in processTarEntry
	ignored, err := matcher.MatchesOrParentMatches(relPathSlash)
	if err != nil {
		t.Errorf("MatchesOrParentMatches error: %v", err)
	}
	return ignored
}

func TestPatternMatcherIntegration(t *testing.T) {
	tests := []struct {
		name           string
		ignorePatterns []string
		path           string
		isDir          bool
		wantIgnored    bool
	}{
		{
			name:           "Simple match",
			ignorePatterns: []string{"*.log"},
			path:           "foo.log",
			isDir:          false,
			wantIgnored:    true,
		},
		{
			name:           "Simple mismatch",
			ignorePatterns: []string{"*.log"},
			path:           "foo.txt",
			isDir:          false,
			wantIgnored:    false,
		},
		{
			name:           "Directory match",
			ignorePatterns: []string{"temp"},
			path:           "temp",
			isDir:          true,
			wantIgnored:    true,
		},
		{
			name:           "Negation",
			ignorePatterns: []string{"*.log", "!important.log"},
			path:           "important.log",
			isDir:          false,
			wantIgnored:    false, 
		},
		{
			name:           "Double star",
			ignorePatterns: []string{"**/*.tmp"},
			path:           "a/b/c/foo.tmp",
			isDir:          false,
			wantIgnored:    true,
		},
		{
			name:           "Directory pattern with slash matching directory",
			ignorePatterns: []string{"foo/"},
			path:           "foo",
			isDir:          true,
			wantIgnored:    true,
		},
		{
			name:           "Directory pattern with slash matching file (KNOWN LIMITATION)",
			ignorePatterns: []string{"foo/"},
			path:           "foo", // file named foo
			isDir:          false,
			wantIgnored:    true, // LIMITATION: moby/patternmatcher matches this even if it shouldn't per strict Docker spec
		},
		{
			name:           "Nested file in ignored directory",
			ignorePatterns: []string{"foo/"},
			path:           "foo/bar",
			isDir:          false,
			wantIgnored:    true, // MatchesOrParentMatches should catch this
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := patternmatcher.New(tt.ignorePatterns)
			if err != nil {
				t.Fatalf("failed to create matcher: %v", err)
			}

			got := testShouldIgnore(t, matcher, tt.path, tt.isDir)
			if got != tt.wantIgnored {
				t.Errorf("testShouldIgnore(%q, isDir=%v) = %v, want %v", tt.path, tt.isDir, got, tt.wantIgnored)
			}
		})
	}
}
