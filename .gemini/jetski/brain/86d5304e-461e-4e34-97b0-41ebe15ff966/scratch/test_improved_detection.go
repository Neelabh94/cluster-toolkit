package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/hashicorp/go-getter"
)

var defaultDetectors = []getter.Detector{
	new(getter.GitHubDetector),
	new(getter.GitDetector),
	new(getter.GCSDetector),
	new(getter.FileDetector), // Added to test detection of local paths
}

func testSource(source string) {
	pwd, _ := os.Getwd()
	detected, err := getter.Detect(source, pwd, defaultDetectors)
	if err != nil {
		fmt.Printf("Source %q -> Error: %v\n", source, err)
		// Try to parse as URL to see if it has a scheme we don't support
		if u, uerr := url.Parse(source); uerr == nil && u.Scheme != "" {
			fmt.Printf("  Hint: Detected scheme %q which may not be supported.\n", u.Scheme)
		}
		return
	}

	fmt.Printf("Source %q -> Detected: %s\n", source, detected)
	
	if strings.HasPrefix(detected, "file://") {
		fmt.Println("  Hint: This looks like a local file. Ensure you use ./ or ../ if you meant to use a local module.")
	}
}

func main() {
	testSource("github.com/foo/bar")
	testSource("gcs::https://www.googleapis.com/storage/v1/bucket/path")
	testSource("s3://my-bucket/path")
	testSource("http://example.com/mod.zip")
	testSource("/tmp/foo")
	testSource("my-modules/foo")
}
