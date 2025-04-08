package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/sourcegraph/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

func main() {
	var projectPath string
	var module string
	var oldVersion string
	var newVersion string

	flag.StringVar(&projectPath, "project-path", "", "Path to your Go project")
	flag.StringVar(&module, "module", "", "Module path of the dependency you want to check")
	flag.StringVar(&oldVersion, "old-version", "", "Old version of the dependency")
	flag.StringVar(&newVersion, "new-version", "", "New version of the dependency")
	flag.Parse()

	myModulePath := projectPath

	myIndexPath, err := generateScipIndex(myModulePath)
	if err != nil {
		log.Fatalf("Failed to generate SCIP index for my module: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(myIndexPath))

	moduleToUpgrade := module

	oldModuleIndexPath, err := cloneAndRunCommand(
		fmt.Sprintf("https://%s.git", moduleToUpgrade),
		oldVersion,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(filepath.Dir(oldModuleIndexPath))

	newModuleIndexPath, err := cloneAndRunCommand(
		fmt.Sprintf("https://%s.git", moduleToUpgrade),
		newVersion,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(filepath.Dir(newModuleIndexPath))

	usedFunctions, err := findUsedFunctions(myIndexPath, oldModuleIndexPath, moduleToUpgrade)
	if err != nil {
		log.Fatalf("Failed to find used functions: %v", err)
	}

	newFunctions, err := getAvailableFunctions(newModuleIndexPath)
	if err != nil {
		log.Fatalf("Failed to find new functions: %v", err)
	}

	changed := findChangedFunctions(usedFunctions, newFunctions)

	if len(changed) > 0 {
		fmt.Println("The following functions have been changed or removed:")
		for fn, newFn := range changed {
			fmt.Println("- " + fn + " -> " + newFn)
		}
	} else {
		fmt.Println("No breaking changes detected.")
	}
}

// generateScipIndex runs scip-go on a module and returns the path to the index file
func generateScipIndex(moduleLocation string) (string, error) {
	outputDir, err := os.MkdirTemp("", "scip-index-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	outputPath := filepath.Join(outputDir, "index.scip")

	// Determine where to run scip-go
	targetPath := moduleLocation

	// Run scip-go
	cmd := exec.Command("scip-go", "--output", outputPath, targetPath)
	cmd.Dir = moduleLocation
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to run scip-go: %w", err)
	}

	return outputPath, nil
}

// findUsedFunctions analyzes a SCIP index to find functions used from a module
func findUsedFunctions(indexPath, oldModuleIndexPath, moduleName string) (map[string]struct{}, error) {
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	var index scip.Index
	if err := proto.Unmarshal(indexData, &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal index: %w", err)
	}

	usedFunctions := make(map[string]struct{})

	// Iterate through documents in the index
	for _, doc := range index.Documents {
		// Iterate through occurrences
		for _, occ := range doc.Occurrences {
			if strings.Contains(occ.Symbol, moduleName) {
				re := regexp.MustCompile(`\w+\(\)\.`)
				match := re.FindString(occ.Symbol)
				if match != "" {
					// remove the ()
					funcName := strings.TrimSuffix(match, "().")
					usedFunctions[funcName] = struct{}{}
				}
			}
		}
	}

	oldModuleIndexData, err := os.ReadFile(oldModuleIndexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read old module index file: %w", err)
	}

	var oldModuleIndex scip.Index
	if err := proto.Unmarshal(oldModuleIndexData, &oldModuleIndex); err != nil {
		return nil, fmt.Errorf("failed to unmarshal old module index: %w", err)
	}

	oldModuleUsedFunctions := make(map[string]struct{})
	// get func signatures
	for _, doc := range oldModuleIndex.Documents {
		for _, sym := range doc.Symbols {
			if len(sym.Documentation) > 0 {
				funcSignature := extractExportedFunctionSignature(sym.Documentation[0])
				oldModuleUsedFunctions[funcSignature] = struct{}{}
			}
		}
	}

	resultMap := make(map[string]struct{})
	for k := range usedFunctions {
		for j := range oldModuleUsedFunctions {
			if strings.Contains(j, k) {
				resultMap[j] = struct{}{}
			}
		}
	}

	return resultMap, nil
}

func extractExportedFunctionSignature(s string) string {
	parts := strings.Split(s, "\n")
	if len(parts) < 2 {
		return ""
	}
	// Extract the function definition between \n characters
	funcDef := parts[1]

	// return empty string if the function is not exported
	// check after the func keyword
	if !strings.Contains(funcDef, "func") {
		return ""
	}

	// Find the function name after "func "
	afterFunc := funcDef[strings.Index(funcDef, "func ")+5:]
	// Get the first word which should be the function name
	funcName := strings.Split(afterFunc, "(")[0]
	funcName = strings.TrimSpace(funcName)

	// Check if function name starts with uppercase (exported)
	if len(funcName) == 0 || !unicode.IsUpper(rune(funcName[0])) {
		return ""
	}

	return funcDef
}

// getAvailableFunctions reads a SCIP index to find all exported functions
func getAvailableFunctions(indexPath string) (map[string]struct{}, error) {
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	var index scip.Index
	if err := proto.Unmarshal(indexData, &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal index: %w", err)
	}

	functions := make(map[string]struct{})

	for _, doc := range index.Documents {
		for _, sym := range doc.Symbols {
			funcSignature := extractExportedFunctionSignature(sym.Documentation[0])
			if funcSignature == "" {
				continue
			}
			functions[funcSignature] = struct{}{}
		}
	}

	return functions, nil
}

// findChangedFunctions compares used functions with available functions and detects signature changes
func findChangedFunctions(
	usedFunctions, newFunctions map[string]struct{},
) map[string]string {
	changed := make(map[string]string)

	for usedFn := range usedFunctions {
		// Extract function name before the parameters
		usedFnName := strings.Split(strings.TrimSpace(usedFn), "(")[0]
		usedFnName = strings.TrimPrefix(usedFnName, "func ")

		// Look for functions with same name but different signatures in new version
		for newFn := range newFunctions {
			newFnName := strings.Split(strings.TrimSpace(newFn), "(")[0]
			newFnName = strings.TrimPrefix(newFnName, "func ")

			if usedFnName == newFnName {
				// Found function with same name, check if signatures differ
				if usedFn != newFn {
					changed[usedFn] = newFn
				}
				break
			}
		}

		// Also mark completely removed functions
		found := false
		for newFn := range newFunctions {
			if strings.Contains(newFn, usedFnName) {
				found = true
				break
			}
		}
		if !found {
			changed[usedFn] = "removed"
		}
	}

	return changed
}

func cloneAndRunCommand(repoURL, version string) (string, error) {
	// Create a temporary directory to clone into
	tempDir, err := os.MkdirTemp("", "repo-clone-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	gitCloneCmd := exec.Command("git", "clone", repoURL, tempDir)
	gitCloneCmd.Stdout = os.Stdout
	gitCloneCmd.Stderr = os.Stderr
	if err := gitCloneCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	// Checkout the specific version (could be a tag, branch, or commit hash)
	gitCheckoutCmd := exec.Command("git", "checkout", version)
	gitCheckoutCmd.Dir = tempDir
	gitCheckoutCmd.Stdout = os.Stdout
	gitCheckoutCmd.Stderr = os.Stderr
	if err := gitCheckoutCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to checkout version %s: %w", version, err)
	}

	// Change directory and run command
	fmt.Printf("Running command 'scip-go' with verbose output in %s...\n", tempDir)
	cmd := exec.Command("scip-go",
		"--verbose",
		"--output", "index.scip",
		"--repository-remote", repoURL,
		"--project-root", tempDir,
		"--repository-root", tempDir,
		"./...", // Index all packages recursively
	)
	cmd.Dir = tempDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}

	outputPath := filepath.Join(tempDir, "index.scip")

	return outputPath, nil
}
