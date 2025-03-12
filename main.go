package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schollz/progressbar/v3"
)

const maxFixesPerFile = 1000

func main() {
	// Initialize and parse command-line arguments
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
		log.Fatal("Argument must be one directory path")
	}
	dir := args[0]

	// Prepare the working directory
	if err := os.Chdir(dir); err != nil {
		log.Fatalf("Failed to change directory to %s: %v", dir, err)
	}

	// Discover Go files
	paths, err := discoverGoFiles()
	if err != nil {
		log.Fatal("Walk error:", err)
	}

	log.Printf("Number of files: %d", len(paths))

	// Analyze files and collect quickfix candidates
	fileStats, err := analyzeFiles(goplsPath, paths)
	if err != nil {
		log.Fatal("Error analyzing files:", err)
	}

	printFileStats(fileStats)

	// Prompt user for confirmation
	if !confirmApplyQuickfixes() {
		log.Println("Aborting - no changes applied")
		os.Exit(0)
	}

	// Apply quickfixes
	err = applyQuickfixes(goplsPath, paths)
	if err != nil {
		log.Fatal("Error applying quickfixes:", err)
	}

	log.Println("All quickfixes applied successfully.")
}

// discoverGoFiles discovers all .go files in the current directory.
func discoverGoFiles() ([]string, error) {
	var paths []string
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
	return paths, err
}

// analyzeFiles analyzes the given files and collects quickfix candidates.
func analyzeFiles(goplsPath string, paths []string) (map[string]int, error) {
	fileStats := make(map[string]int)
	statsMutex := sync.Mutex{}
	jobsStat := make(chan string, 100)
	wgStat := sync.WaitGroup{}

	barStat := progressbar.Default(int64(len(paths)))

	for range 10 {
		wgStat.Add(1)
		go workerStat(goplsPath, jobsStat, fileStats, &statsMutex, &wgStat, barStat)
	}

	for _, p := range paths {
		jobsStat <- p
	}
	close(jobsStat)
	wgStat.Wait()

	return fileStats, nil
}

// printFileStats prints the collected file statistics.
func printFileStats(fileStats map[string]int) {
	log.Printf("Total quickfix candidates found:")
	for line, count := range fileStats {
		log.Printf("%s: %d", line, count)
	}
}

// confirmApplyQuickfixes prompts the user for confirmation before applying quickfixes.
func confirmApplyQuickfixes() bool {
	fmt.Print("\nFound quickfix candidates. Proceed to apply? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(input)) == "y"
}

// applyQuickfixes applies the quickfixes to the given files.
func applyQuickfixes(goplsPath string, paths []string) error {
	jobsExec := make(chan string, 100)
	wgExec := sync.WaitGroup{}

	bar := progressbar.Default(int64(len(paths)))

	for range 10 {
		wgExec.Add(1)
		go worker(goplsPath, jobsExec, &wgExec, bar)
	}

	for _, p := range paths {
		jobsExec <- p
	}
	close(jobsExec)
	wgExec.Wait()
	return nil
}

// workerStat is a worker goroutine that analyzes a single file and collects quickfix candidates.
func workerStat(
	goplsPath string,
	jobs <-chan string,
	fileStats map[string]int,
	statsMutex *sync.Mutex,
	wg *sync.WaitGroup,
	bar *progressbar.ProgressBar,
) {
	defer wg.Done()

	for path := range jobs {
		cmd := exec.Command(goplsPath, "codeaction", "-kind=quickfix", path)
		output, err := cmd.CombinedOutput()

		if err != nil {
			if !strings.Contains(string(output), "no matching code action") {
				log.Printf("Error checking %s: %v\n%s", path, err, string(output))
				continue
			}
		} else {
			lines := strings.SplitSeq(strings.TrimSpace(string(output)), "\n")
			for line := range lines {
				if line != "" {
					statsMutex.Lock()
					fileStats[line]++
					statsMutex.Unlock()
				}
			}
		}
		bar.Add(1)
	}
}

// worker is a worker goroutine that applies quickfixes to a single file.
func worker(goplsPath string, jobs <-chan string, wg *sync.WaitGroup, bar *progressbar.ProgressBar) {
	defer wg.Done()

	for path := range jobs {
		for range maxFixesPerFile {
			cmd := exec.Command(goplsPath, "codeaction", "-kind=quickfix", "-exec", "-w", path)
			output, err := cmd.CombinedOutput()

			if err != nil {
				if !strings.Contains(string(output), "no matching code action") {
					log.Printf("Error in %s: %v\n%s", path, err, string(output))
				} 
                break
			}
		}

		bar.Add(1)
	}
}
