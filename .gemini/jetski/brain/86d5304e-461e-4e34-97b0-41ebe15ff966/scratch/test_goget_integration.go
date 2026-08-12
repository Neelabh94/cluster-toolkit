package main

import (
	"fmt"
	"hpc-toolkit/pkg/sourcereader"
	"os"
)

func main() {
	r := sourcereader.GoGetterSourceReader{}
	
	source := "github.com/GoogleCloudPlatform/cluster-toolkit//modules/spack"
	if len(os.Args) > 1 {
		source = os.Args[1]
	}
	
	// Test case 1: Remote Git module (should fail if git is missing)
	err := r.GetModule(source, "/tmp/dst1")
	fmt.Printf("Remote Module %q -> Error: %v\n", source, err)
	
	// Test case 2: Local module (should succeed if file exists, or fail with local-specific error)
	err = r.GetModule("./modules/spack", "/tmp/dst2")
	fmt.Printf("Local Module -> Error: %v\n", err)
}
