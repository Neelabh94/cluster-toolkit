package main

import (
	"fmt"
	"os"
	"github.com/hashicorp/go-getter"
)

func main() {
	pwd, _ := os.Getwd()
	
	// Test Detect with nil detectors
	detected, err := getter.Detect("github.com/user/repo", pwd, nil)
	if err != nil {
		fmt.Printf("Error detecting: %v\n", err)
	} else {
		fmt.Printf("Detected: %q\n", detected)
	}
}
