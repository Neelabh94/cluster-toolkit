package main

import (
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/go-getter"
)

func main() {
	// Create a dummy file to fetch
	os.WriteFile("/tmp/dummy-mod", []byte("hello"), 0644)
	defer os.Remove("/tmp/dummy-mod")

	source := "/tmp/dummy-mod"
	dst := "/tmp/test-get-fallback"
	os.RemoveAll(dst)

	client := getter.Client{
		Src: source,
		Dst: dst,
		Pwd: dst,
		Mode: getter.ClientModeAny,
		Detectors: []getter.Detector{
			new(getter.GitHubDetector),
			new(getter.GitDetector),
			new(getter.GCSDetector),
			new(getter.FileDetector),
		},
		Getters: map[string]getter.Getter{
			"git": &getter.GitGetter{Timeout: 1 * time.Minute},
			// No file getter registered here either, let's see what happens
		},
	}

	err := client.Get()
	if err != nil {
		fmt.Printf("Source %s -> Error: %v\n", source, err)
	} else {
		fmt.Printf("Source %s -> Success!\n", source)
	}
	
	// What if source is relative but doesn't start with ./
	os.MkdirAll("/tmp/test-relative/my-mod", 0755)
	os.WriteFile("/tmp/test-relative/my-mod/hello.txt", []byte("hello"), 0644)
	defer os.RemoveAll("/tmp/test-relative")
	
	wd, _ := os.Getwd()
	os.Chdir("/tmp/test-relative")
	defer os.Chdir(wd)
	
	source2 := "my-mod"
	dst2 := "/tmp/test-get-fallback2"
	os.RemoveAll(dst2)
	
	client2 := getter.Client{
		Src: source2,
		Dst: dst2,
		Pwd: dst2,
		Mode: getter.ClientModeAny,
		Detectors: []getter.Detector{
			new(getter.GitHubDetector),
			new(getter.GitDetector),
			new(getter.GCSDetector),
			new(getter.FileDetector),
		},
		Getters: map[string]getter.Getter{
			"git": &getter.GitGetter{Timeout: 1 * time.Minute},
		},
	}
	
	err = client2.Get()
	if err != nil {
		fmt.Printf("Source2 %s -> Error: %v\n", source2, err)
	} else {
		fmt.Printf("Source2 %s -> Success!\n", source2)
	}
}
