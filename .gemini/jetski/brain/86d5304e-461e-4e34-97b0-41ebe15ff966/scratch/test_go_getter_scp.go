package main

import (
	"context"
	"fmt"
	"os"
	"time"
	"github.com/hashicorp/go-getter"
)

func main() {
	pwd, _ := os.Getwd()
	dst := "/tmp/test-scp-download"
	os.RemoveAll(dst)

	client := &getter.Client{
		Src: "git::github.com:not/exist.git",
		Dst: dst,
		Pwd: pwd,
		Mode: getter.ClientModeAny,
		Getters: map[string]getter.Getter{
			"git": &getter.GitGetter{Timeout: 5 * time.Minute},
		},
		Ctx: context.Background(),
	}

	err := client.Get()
	if err != nil {
		fmt.Printf("Error downloading: %v\n", err)
	} else {
		fmt.Printf("Downloaded successfully!\n")
	}
}
