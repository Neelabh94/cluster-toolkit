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
	fmt.Println()
}

func main() {
	testParse("gcs::https://www.googleapis.com/storage/v1/bucket/path")
	testParse("git::https://github.com/foo/bar.git")
	testParse("github.com/foo/bar")
	testParse("git::https://github.com/GoogleCloudPlatform/cluster-toolkit.git//modules/spack")
	testParse("git+ssh://git@github.com/user/repo.git")
	testParse("git+https://github.com/user/repo.git")
	testParse("github.com:not/exist.git")
}
