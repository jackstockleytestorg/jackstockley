package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

var (
	basePath   = "namespaces/live.cloud-platform.service.justice.gov.uk"
	branchName = flag.String("branch", os.Getenv("BRANCH_NAME"), "The branch name to search")
	help       = flag.Bool("h", false, "Show help message")

	requiredTags = []string{
		"business-unit",
		"application",
		"is-production",
		"owner",
		"namespace",
		"service-area",
		"source-code",
		"slack-channel",
	}

	awsProviderPattern = regexp.MustCompile(`(?s)provider\s+"aws"\s*\{(?:[^{}]|\{[^{}]*\})*\}`)
	tagPattern         = regexp.MustCompile(`(?m)^\s*"?([a-zA-Z][a-zA-Z0-9_-]*)"?\s*=`)
)

func main() {
	flag.Usage = printUsage
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if *branchName == "" {
		fmt.Fprintln(os.Stderr, "Error: BRANCH_NAME must be set.")
		flag.Usage()
		os.Exit(1)
	}

	namespace, err := getNamespace(*branchName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Could not determine namespace: %v\n", err)
		os.Exit(1)
	}

	resourcePath := filepath.Join(basePath, namespace, "resources", "main.tf")

	fmt.Printf("Searching for default_tags in %s on branch %s...\n\n", resourcePath, *branchName)

	content, err := getFileFromBranch(*branchName, resourcePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Could not read file from branch: %v\n", err)
		os.Exit(1)
	}

	providers, err := checkAllAwsProviders(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ All %d AWS provider(s) have the required tags\n\n", len(providers))
	for _, provider := range providers {
		fmt.Printf("Provider: %s\n", provider.name)
		fmt.Println("Tags:")
		for _, tag := range provider.tags {
			fmt.Printf("  ✓ %s\n", tag)
		}
		fmt.Println()
	}
}

// printUsage displays the help message for the command-line tool.
func printUsage() {
	fmt.Fprintf(os.Stderr, "Branch Default Tags Checker\n")
	fmt.Fprintf(os.Stderr, "===========================\n\n")
	fmt.Fprintf(os.Stderr, "This tool searches a git branch for default_tags in Terraform main.tf files.\n\n")
	fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
	fmt.Fprintf(os.Stderr, "  NAMESPACE    - The namespace to search\n")
	fmt.Fprintf(os.Stderr, "  BRANCH_NAME  - The branch name to search\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s -namespace=my-namespace -branch=my-branch\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  NAMESPACE=my-namespace BRANCH_NAME=my-branch %s\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -h\n\n", os.Args[0])
}

// getFileFromBranch retrieves the content of a file from a specific git branch using git show.
func getFileFromBranch(branch, filePath string) (string, error) {
	cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", branch, filePath))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git show failed: %w - %s", err, string(output))
	}
	return string(output), nil
}

type providerInfo struct {
	name string
	tags []string
}

// checkAllAwsProviders finds all AWS providers and verifies each has all required tags
func checkAllAwsProviders(content string) ([]providerInfo, error) {
	var providers []providerInfo
	var errors []string

	// Find all AWS provider blocks
	lines := regexp.MustCompile(`provider\s+"aws"`).FindAllStringIndex(content, -1)

	for _, loc := range lines {
		// Extract the provider block starting from this location
		start := loc[0]
		providerBlock := extractProviderBlock(content[start:])

		// Extract alias if present
		aliasRegex := regexp.MustCompile(`alias\s*=\s*"([^"]+)"`)
		aliasMatch := aliasRegex.FindStringSubmatch(providerBlock)

		providerName := "aws (default)"
		if len(aliasMatch) > 1 {
			providerName = fmt.Sprintf("aws (alias: %s)", aliasMatch[1])
		}

		// Extract tags from default_tags block
		tagsRegex := regexp.MustCompile(`(?s)default_tags\s*\{\s*tags\s*=\s*\{([^}]+)\}`)
		tagsMatch := tagsRegex.FindStringSubmatch(providerBlock)

		if len(tagsMatch) > 1 {
			tags := extractTags(tagsMatch[1])
			providers = append(providers, providerInfo{
				name: providerName,
				tags: tags,
			})

			missing := findMissingTags(tags)
			if len(missing) > 0 {
				errors = append(errors, fmt.Sprintf("❌ Provider '%s' is missing tags: %v", providerName, missing))
			}
		} else {
			// Provider doesn't have default_tags at all
			errors = append(errors, fmt.Sprintf("❌ Provider '%s' does not have default_tags block with all required tags", providerName))
		}
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("❌ No AWS providers found in the file")
	}

	if len(errors) > 0 {
		return nil, fmt.Errorf("%s", joinErrors(errors))
	}

	return providers, nil
}

// extractProviderBlock extracts a provider block by counting braces
func extractProviderBlock(content string) string {
	braceCount := 0
	inBlock := false

	for i, ch := range content {
		if ch == '{' {
			if !inBlock {
				inBlock = true
			}
			braceCount++
		} else if ch == '}' {
			braceCount--
			if braceCount == 0 && inBlock {
				return content[:i+1]
			}
		}
	}
	return content
} // findMissingTags returns tags that are required but not found in the provided list
func findMissingTags(foundTags []string) []string {
	tagMap := make(map[string]bool)
	for _, tag := range foundTags {
		tagMap[tag] = true
	}

	var missing []string
	for _, required := range requiredTags {
		if !tagMap[required] {
			missing = append(missing, required)
		}
	}
	return missing
}

// joinErrors combines multiple error messages into one
func joinErrors(errors []string) string {
	result := ""
	for i, err := range errors {
		if i > 0 {
			result += "\n"
		}
		result += err
	}
	return result
}

// extractTags parses a tag block and extracts individual tag names using regex pattern matching.
func extractTags(tagBlock string) []string {
	var tags []string

	matches := tagPattern.FindAllStringSubmatch(tagBlock, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tags = append(tags, match[1])
		}
	}

	return tags
}

func getNamespace(branch string) (string, error) {
	// get namespace from file path in the github pull request changed files `namespaces/live.cloud-platform.service.justice.gov.uk/<namespace>/...`

	cmd := exec.Command("git", "diff", "main..."+branch, "--name-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w - %s", err, string(output))
	}

	changedFiles := string(output)

	pattern := regexp.MustCompile(`namespaces/live\.cloud-platform\.service\.justice\.gov\.uk/([^/]+)/`)
	matches := pattern.FindStringSubmatch(changedFiles)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("could not extract namespace from changed files in branch: %s", branch)
}
