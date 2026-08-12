package main

import (
	"fmt"
	"net/url"
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
	
	u, err := url.Parse(detected)
	if err != nil {
		fmt.Printf("  Error parsing detected: %v\n", err)
		return
	}
	fmt.Printf("  Scheme: %q\n", u.Scheme)
}

func main() {
	testSource("gs://my-bucket/path")
}
