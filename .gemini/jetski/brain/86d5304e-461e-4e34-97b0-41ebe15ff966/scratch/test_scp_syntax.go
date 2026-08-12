package main

import (
	"fmt"
	"os"
	"github.com/hashicorp/go-getter"
)

func main() {
	pwd, _ := os.Getwd()
	var myDetectors = []getter.Detector{
		new(getter.GitHubDetector),
		new(getter.GitLabDetector),
		new(getter.GitDetector),
		new(getter.GCSDetector),
	}
	
	detected, err := getter.Detect("git@github.com:not/exist.git", pwd, myDetectors)
	if err != nil {
		fmt.Printf("Git@ SCP Error detecting: %v\n", err)
	} else {
		fmt.Printf("Git@ SCP Detected: %q\n", detected)
	}
}
