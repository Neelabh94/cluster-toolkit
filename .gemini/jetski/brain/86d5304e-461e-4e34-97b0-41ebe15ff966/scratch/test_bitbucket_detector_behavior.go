package main

import (
	"fmt"
	"os"
	"github.com/hashicorp/go-getter"
)

func main() {
	d := new(getter.BitBucketDetector)
	pwd, _ := os.Getwd()
	
	detected, ok, err := d.Detect("bitbucket.org/user/repo", pwd)
	if err != nil {
		fmt.Printf("BitBucketDetector Error: %v\n", err)
	} else {
		fmt.Printf("BitBucketDetector Detected: %q, OK: %v\n", detected, ok)
	}
}
