package main

import (
	"fmt"
	"os"
	"github.com/hashicorp/go-getter"
)

func main() {
	d := new(getter.GitHubDetector)
	pwd, _ := os.Getwd()
	
	detected, ok, err := d.Detect("github.com/user/repo", pwd)
	if err != nil {
		fmt.Printf("GitHubDetector Error: %v\n", err)
	} else {
		fmt.Printf("GitHubDetector Detected: %q, OK: %v\n", detected, ok)
	}
}
