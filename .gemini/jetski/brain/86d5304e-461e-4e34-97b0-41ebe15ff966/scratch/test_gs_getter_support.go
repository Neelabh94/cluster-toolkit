package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-getter"
)

func main() {
	client := getter.Client{
		Src: "gs://some-bucket/some-path",
		Dst: "/tmp/test-gs-getter",
		Pwd: "/tmp/test-gs-getter",
		Mode: getter.ClientModeAny,
		Getters: map[string]getter.Getter{
			"gcs": &getter.GCSGetter{Timeout: 1 * time.Minute},
		},
		Ctx: context.Background(),
	}

	err := client.Get()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Success!\n")
	}
}
