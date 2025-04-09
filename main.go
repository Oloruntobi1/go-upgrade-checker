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

	"github.com/google/go-cmp/cmp"
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

	projectIndexPath, err := generateScipIndex(projectPath)
	if err != nil {
		os.RemoveAll(projectIndexPath)
		log.Fatalf("Failed to generate SCIP index for my module: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(projectIndexPath))

	// Clone repository once
	repoDir, err := os.MkdirTemp("", "repo-clone-*")
	if err != nil {
		os.RemoveAll(repoDir)
		log.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoDir)

	repoURL := fmt.Sprintf("https://%s.git", module)
	gitCloneCmd := exec.Command("git", "clone", repoURL, repoDir)
	gitCloneCmd.Stderr = os.Stderr
	if err := gitCloneCmd.Run(); err != nil {
		os.RemoveAll(repoDir)
		log.Fatalf("Failed to clone repository: %v", err)
	}

	// Generate index for old version
	oldModuleIndexPath, err := generateIndexForVersion(repoDir, oldVersion)
	if err != nil {
		os.RemoveAll(oldModuleIndexPath)
		log.Fatalf("Failed to generate index for old version: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(oldModuleIndexPath))

	// Generate index for new version
	newModuleIndexPath, err := generateIndexForVersion(repoDir, newVersion)
	if err != nil {
		os.RemoveAll(newModuleIndexPath)
		log.Fatalf("Failed to generate index for new version: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(newModuleIndexPath))

	usedSymbols, err := findUsedSymbols(projectIndexPath, oldModuleIndexPath, module)
	if err != nil {
		log.Fatalf("Failed to find used symbols: %v", err)
	}

	newSymbols, err := getAvailableSymbols(newModuleIndexPath)
	if err != nil {
		log.Fatalf("Failed to find used symbols: %v", err)
	}

	added, removed := findChangedSymbols(usedSymbols, newSymbols)

	fmt.Println()

	if len(added) > 0 || len(removed) > 0 {
		fmt.Println("The following symbols have been changed or removed:")
		fmt.Println("Added:")
		for sym, newSym := range added {
			fmt.Println("- " + sym + " -> " + newSym)
		}
		fmt.Println("Removed:")
		for sym, newSym := range removed {
			fmt.Println("- " + sym + " -> " + newSym)
		}
	} else {
		fmt.Println("No breaking changes detected.")
	}
}

// generateIndexForVersion checks out a specific version and generates its SCIP index
func generateIndexForVersion(repoDir, version string) (string, error) {
	// Checkout the specific version
	gitCheckoutCmd := exec.Command("git", "checkout", version)
	gitCheckoutCmd.Dir = repoDir
	gitCheckoutCmd.Stderr = os.Stderr
	if err := gitCheckoutCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to checkout version %s: %w", version, err)
	}

	// Create output directory for the index
	outputDir, err := os.MkdirTemp("", "scip-index-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	outputPath := filepath.Join(outputDir, "index.scip")

	// Run scip-go
	cmd := exec.Command("scip-go",
		"--verbose",
		"--output", outputPath,
		"--project-root", repoDir,
		"--repository-root", repoDir,
		"./...", // Index all packages recursively
	)
	cmd.Dir = repoDir
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		os.RemoveAll(outputDir)
		return "", fmt.Errorf("failed to run scip-go: %w", err)
	}

	return outputPath, nil
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
		os.RemoveAll(outputDir)
		return "", fmt.Errorf("failed to run scip-go: %w", err)
	}

	return outputPath, nil
}

// findUsedSymbols analyzes the user project's SCIP index to find symbols it uses
// that originate from the specified targetModule
func findUsedSymbols(indexPath, oldModuleIndexPath, moduleName string) (map[string][]string, error) {
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read user index file '%s': %w", indexPath, err)
	}

	var index scip.Index
	if err := proto.Unmarshal(indexData, &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user index '%s': %w", indexPath, err)
	}

	usedSymbols := make(map[string][]string)

	for _, doc := range index.Documents {
		for _, occ := range doc.Occurrences {
			if strings.Contains(occ.Symbol, moduleName) {
				val, typ := extractSymbolsFromOccurrence(occ.Symbol)
				if val != "" {
					field := val
					if typ == "type" {
						val = strings.Split(val, "#")[0]
						if len(strings.Split(val, ".")) > 1 {
							field = strings.Split(val, ".")[1]
						}
						usedSymbols[val] = append(usedSymbols[val], field)
					} else {
						usedSymbols[val] = append(usedSymbols[val], "")
					}
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

	oldModuleUsedSymbols := make(map[string][]string)

	for _, doc := range oldModuleIndex.Documents {
		for _, sym := range doc.Symbols {
			val, typ := extractSymbolsFromOccurrence(sym.Symbol)
			if val != "" {
				if len(sym.Documentation) > 0 {
					def := extractSymbolDefinition(sym.Documentation[0])
					if def != "" {
						if typ == "type" {
							d := strings.Split(val, "#")[0]
							if len(strings.Split(val, "#")) > 1 {
								oldModuleUsedSymbols[d] = append(oldModuleUsedSymbols[d], def)
							}
						} else {
							oldModuleUsedSymbols[val] = append(oldModuleUsedSymbols[val], def)
						}
					}
				}
			}
		}
	}

	resultMap := make(map[string][]string)
	for k := range usedSymbols {
		for j, v := range oldModuleUsedSymbols {
			if strings.Contains(j, k) {
				resultMap[j] = v
			}
		}
	}

	return resultMap, nil
}

func determineSymbolType(symbol string) string {
	switch {
	case strings.Contains(symbol, "()"):
		return "function"
	case strings.Contains(symbol, "#"):
		return "type"
	default:
		return "constant or variable"
	}
}

func extractSymbolDefinition(symbol string) string {
	parts := strings.Split(symbol, "\n")
	if len(parts) < 2 {
		return ""
	}
	// Extract the function definition between \n characters
	symbolDef := parts[1]

	return symbolDef
}

func extractSymbolsFromOccurrence(symbol string) (string, string) {
	re := regexp.MustCompile("`[^`]+`(/[^\\s`]+?\\.)")
	matches := re.FindAllStringSubmatch(symbol, -1)
	for _, match := range matches {
		if len(match) > 1 {
			symbolType := determineSymbolType(match[1])
			var val string
			if symbolType == "function" {
				val = strings.TrimPrefix(match[1], "/")
				val = strings.TrimSuffix(val, "().")
			} else if symbolType == "type" {
				val = strings.TrimPrefix(match[1], "/")
				val = strings.TrimSuffix(val, ".")
			} else {
				val = strings.TrimPrefix(match[1], "/")
				val = strings.TrimSuffix(val, ".")
			}
			return val, symbolType
		}
	}
	return "", ""
}

func getAvailableSymbols(indexPath string) (map[string][]string, error) {
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	var index scip.Index
	if err := proto.Unmarshal(indexData, &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal index: %w", err)
	}

	symbols := make(map[string][]string)

	for _, doc := range index.Documents {
		for _, sym := range doc.Symbols {
			val, typ := extractSymbolsFromOccurrence(sym.Symbol)
			if val != "" {
				if len(sym.Documentation) > 0 {
					def := extractSymbolDefinition(sym.Documentation[0])
					if def != "" {
						if typ == "type" {
							d := strings.Split(val, "#")[0]
							if len(strings.Split(val, "#")) > 1 {
								symbols[d] = append(symbols[d], def)
							}
						} else {
							symbols[val] = append(symbols[val], def)
						}
					}
				}
			}
		}
	}

	return symbols, nil
}

func findChangedSymbols(oldSymbols map[string][]string, newSymbols map[string][]string) (map[string]string, map[string]string) {
	added := make(map[string]string)
	removed := make(map[string]string)

	for oldSymbol, oldSymbolDefs := range oldSymbols {
		_, exists := newSymbols[oldSymbol]
		if exists {
			if cmp.Equal(oldSymbolDefs, newSymbols[oldSymbol]) {
				continue
			} else {
				a, b := difference(oldSymbolDefs, newSymbols[oldSymbol])
				if len(a) > 0 {
					removed[oldSymbol] = a[0]
				}
				if len(b) > 0 {
					added[oldSymbol] = b[0]
				}
			}
		}

		// Also mark completely removed functions
		found := false
		for newFn := range newSymbols {
			if strings.Contains(newFn, oldSymbol) {
				found = true
				break
			}
		}
		if !found {
			removed[oldSymbol] = "removed"
		}
	}

	return added, removed
}

// difference returns two slices:
// - items in a but not in b
// - items in b but not in a
func difference(a, b []string) ([]string, []string) {
	aMap := make(map[string]bool)
	bMap := make(map[string]bool)

	for _, item := range a {
		aMap[item] = true
	}
	for _, item := range b {
		bMap[item] = true
	}

	var onlyInA []string
	var onlyInB []string

	for _, item := range a {
		if !bMap[item] {
			onlyInA = append(onlyInA, item)
		}
	}

	for _, item := range b {
		if !aMap[item] {
			onlyInB = append(onlyInB, item)
		}
	}

	return onlyInA, onlyInB
}
