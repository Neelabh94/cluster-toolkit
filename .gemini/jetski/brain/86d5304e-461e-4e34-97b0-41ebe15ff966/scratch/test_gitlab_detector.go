package main

import (
	"fmt"
	"github.com/hashicorp/go-getter"
)

func main() {
	_ = new(getter.GitLabDetector)
	fmt.Println("GitLabDetector exists")
}
