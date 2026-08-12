package main

import (
	"fmt"
	"net/url"
)

func testParse(uStr string) {
	u, err := url.Parse(uStr)
	if err != nil {
		fmt.Printf("Error parsing %q: %v\n", uStr, err)
		return
	}
	fmt.Printf("URL: %q\n", uStr)
	fmt.Printf("  Scheme: %q\n", u.Scheme)
	fmt.Printf("  Opaque: %q\n", u.Opaque)
	fmt.Printf("  Host:   %q\n", u.Host)
	fmt.Printf("  Path:   %q\n", u.Path)
	fmt.Println()
}

func main() {
	testParse("git::https://github.com/foo/bar.git")
	testParse("gcs::https://www.googleapis.com/storage/v1/bucket/path")
	testParse("git+ssh://git@github.com/foo/bar.git")
}
