package main

import (
	"fmt"
	"github.com/hashicorp/go-getter"
)

func main() {
	_ = new(getter.BitBucketDetector)
	fmt.Println("BitBucketDetector exists")
}
