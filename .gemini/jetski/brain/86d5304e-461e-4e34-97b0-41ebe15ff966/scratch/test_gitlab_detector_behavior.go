package main

import (
	"fmt"
	"os"
	"github.com/hashicorp/go-getter"
)

func main() {
	// Test GitHubDetector
	d := new(getter.GitHubDetector)
	pwd, _ := os.Getwd()
	
	detected, ok, err := d.Detect("github.com/user/repo", pwd)
	if err != nil {
		fmt.Printf("GitHubDetector Error: %v\n", err)
	} else {
		fmt.Printf("GitHubDetector Detected: %q, OK: %v\n", detected, ok)
	}

	// Test GitDetector
	gd := new(getter.GitDetector)
	detected, ok, err = gd.Detect("github.com/user/repo", pwd)
	if err != nil {
		fmt.Printf("GitDetector Error: %v\n", err)
	} else {
		fmt.Printf("GitDetector Detected: %q, OK: %v\n", detected, ok)
	}
}
