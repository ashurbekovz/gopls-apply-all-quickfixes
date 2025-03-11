package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func main() {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		log.Fatal("GOPATH is not set")
	}

	goplsPath := filepath.Join(gopath, "bin", "gopls")
	if _, err := os.Stat(goplsPath); os.IsNotExist(err) {
		log.Fatalf("Gopls not found at %s", goplsPath)
	}

	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		log.Fatal("Argument must be one ")
	}
	dir := args[0]

	if err := os.Chdir(dir); err != nil {
		log.Fatalf("Failed to change directory to %s: %v", dir, err)
	}

	paths := []string{}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("Error walking to %s: %v", path, err)
		}

		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}

		paths = append(paths, path)
		return nil
	})

	if err != nil {
		log.Fatal("Walk error:", err)
	}
	log.Printf("Number of files: %d", len(paths))

	jobs := make(chan string, 100)
	var wg sync.WaitGroup
	for w := 1; w <= 10; w++ {
		wg.Add(1)
		go worker(w, goplsPath, jobs, &wg)
	}

	for _, p := range paths {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
}

func worker(id int, goplsPath string, jobs <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	for path := range jobs {
		for {
			cmd := exec.Command(goplsPath, "codeaction", "-kind=quickfix", "-exec", "-w", path)
			output, err := cmd.CombinedOutput()

			if err != nil {
				if strings.Contains(string(output), "no matching code action") {
					break
				}

				log.Printf("Error in %s: %v\n%s", path, err, string(output))
				break
			}

			log.Printf("Worker %d applied quickfix to %s\n", id, path)
		}
	}
}
