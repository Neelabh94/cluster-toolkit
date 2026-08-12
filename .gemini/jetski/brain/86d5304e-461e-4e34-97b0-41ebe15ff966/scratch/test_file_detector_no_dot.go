package main

import (
	"fmt"
	"os"
	"github.com/hashicorp/go-getter"
)

func main() {
	pwd, _ := os.Getwd()
	
	// Create a dummy dir
	os.MkdirAll("test_dummy_dir", 0755)
	defer os.RemoveAll("test_dummy_dir")

	// Test Detect on local dir without ./ using our defaultDetectors
	// Note: We need to define them here or import them, but we can just copy them for this test.
	var myDetectors = []getter.Detector{
		new(getter.GitHubDetector),
		new(getter.GitLabDetector),
		new(getter.GitDetector),
		new(getter.GCSDetector),
	}
	detected, err := getter.Detect("/tmp/test_dummy_dir", pwd, myDetectors)
	if err != nil {
		fmt.Printf("Absolute Path Error detecting: %v\n", err)
	} else {
		fmt.Printf("Absolute Path Detected: %q\n", detected)
	}
}
