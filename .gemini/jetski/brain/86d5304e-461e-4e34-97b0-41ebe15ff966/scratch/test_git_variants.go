package main

import (
	"fmt"
	"os"

	"github.com/hashicorp/go-getter"
)

var defaultDetectors = []getter.Detector{
	new(getter.GitHubDetector),
	new(getter.GitDetector),
	new(getter.GCSDetector),
}

func testSource(source string) {
	pwd, _ := os.Getwd()
	detected, err := getter.Detect(source, pwd, defaultDetectors)
	if err != nil {
		fmt.Printf("Source %q -> Error: %v\n", source, err)
		return
	}
	fmt.Printf("Source %q -> Detected: %s\n", source, detected)
}

func main() {
	testSource("https://gitlab.com/foo/bar.git")
	testSource("git@github.com:foo/bar.git")
}
