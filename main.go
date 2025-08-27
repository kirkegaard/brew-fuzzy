package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Cache struct {
	Packages    []string  `json:"packages"`
	LastUpdated time.Time `json:"last_updated"`
	BrewUpdate  time.Time `json:"brew_update"`
}

type Config struct {
	CacheDir     string
	CacheMaxAge  time.Duration
	InfoCacheDir string
}

func main() {
	config := &Config{
		CacheDir:     filepath.Join(os.Getenv("HOME"), ".cache", "brew-fuzzy"),
		CacheMaxAge:  24 * time.Hour,
		InfoCacheDir: filepath.Join(os.Getenv("HOME"), ".cache", "brew-fuzzy", "info"),
	}

	dryRun := false
	previewColors := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--refresh":
			if _, err := refreshCache(config); err != nil {
				log.Fatalf("Failed to refresh cache: %v", err)
			}
			fmt.Println("Cache refreshed successfully")
			return
		case "--dry-run":
			dryRun = true
		case "--preview-colors":
			previewColors = true
		case "--help", "-h":
			fmt.Println("Brew Fuzzy Install - Lightning-fast TUI for Homebrew package search and installation")
			fmt.Println()
			fmt.Println("Usage:")
			fmt.Println("  ./brew-fuzzy                Launch the fuzzy finder (clean mode)")
			fmt.Println("  ./brew-fuzzy --preview-colors Launch with colorized preview")
			fmt.Println("  ./brew-fuzzy --refresh       Refresh package cache manually")
			fmt.Println("  ./brew-fuzzy --dry-run       Test mode (don't actually install)")
			fmt.Println("  ./brew-fuzzy --help          Show this help message")
			fmt.Println()
			fmt.Println("Controls:")
			fmt.Println("  Type: Search packages (fuzzy matching)")
			fmt.Println("  ↑/↓:  Navigate results")
			fmt.Println("  Tab:  Toggle preview pane")
			fmt.Println("  Enter: Install selected package")
			fmt.Println("  Escape: Cancel and exit")
			fmt.Println()
			fmt.Println("Cache Location: ~/.cache/brew-fuzzy/")
			return
		}
	}

	packages, err := getPackages(config)
	if err != nil {
		log.Fatalf("Failed to get packages: %v", err)
	}

	selected, err := runFzf(packages, config, previewColors)
	if err != nil {
		log.Fatalf("Failed to run fzf: %v", err)
	}

	if selected == "" {
		return
	}

	if err := installPackage(selected, dryRun); err != nil {
		log.Fatalf("Failed to install package: %v", err)
	}
}

func getPackages(config *Config) ([]string, error) {
	cacheFile := filepath.Join(config.CacheDir, "cache.json")

	if cache, err := loadCache(cacheFile); err == nil {
		if time.Since(cache.LastUpdated) < config.CacheMaxAge {
			go refreshCacheBackground(config)
			return cache.Packages, nil
		}
	}

	fmt.Println("Building package cache... (this may take a moment)")
	return refreshCache(config)
}

func loadCache(cacheFile string) (*Cache, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}

	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

func refreshCache(config *Config) ([]string, error) {
	if err := os.MkdirAll(config.CacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}

	if err := os.MkdirAll(config.InfoCacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create info cache dir: %w", err)
	}

	var wg sync.WaitGroup
	var formulae, casks []string
	var formulaeErr, casksErr error

	wg.Add(2)

	go func() {
		defer wg.Done()
		formulae, formulaeErr = getBrewPackages("--formula")
	}()

	go func() {
		defer wg.Done()
		casks, casksErr = getBrewPackages("--cask")
	}()

	wg.Wait()

	if formulaeErr != nil {
		return nil, fmt.Errorf("failed to get formulae: %w", formulaeErr)
	}
	if casksErr != nil {
		return nil, fmt.Errorf("failed to get casks: %w", casksErr)
	}

	packages := append(formulae, casks...)

	cache := Cache{
		Packages:    packages,
		LastUpdated: time.Now(),
		BrewUpdate:  getBrewUpdateTime(),
	}

	cacheFile := filepath.Join(config.CacheDir, "cache.json")
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write cache: %w", err)
	}

	return packages, nil
}

func refreshCacheBackground(config *Config) {
	cache, err := loadCache(filepath.Join(config.CacheDir, "cache.json"))
	if err != nil {
		return
	}

	brewUpdateTime := getBrewUpdateTime()
	if !brewUpdateTime.After(cache.BrewUpdate) {
		return
	}

	go func() {
		_, _ = refreshCache(config)
	}()
}

func getBrewPackages(packageType string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "brew", "search", packageType, ".")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run brew search: %w", err)
	}

	var packages []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			packages = append(packages, line)
		}
	}

	return packages, scanner.Err()
}

func getBrewUpdateTime() time.Time {
	cmd := exec.Command("brew", "--repository")
	output, err := cmd.Output()
	if err != nil {
		return time.Time{}
	}

	gitDir := filepath.Join(strings.TrimSpace(string(output)), ".git", "FETCH_HEAD")
	info, err := os.Stat(gitDir)
	if err != nil {
		return time.Time{}
	}

	return info.ModTime()
}

func runFzf(packages []string, config *Config, previewColors bool) (string, error) {
	previewScript := filepath.Join(config.CacheDir, "preview.sh")
	if err := createPreviewScript(previewScript, config.InfoCacheDir, previewColors); err != nil {
		return "", fmt.Errorf("failed to create preview script: %w", err)
	}

	fzfArgs := []string{
		"--height=80%",
		"--layout=reverse",
		"--info=inline",
		"--border=rounded",
		"--preview=" + previewScript + " {}",
		"--preview-window=right:60%:wrap:border-left",
		"--prompt=🍺 Search packages: ",
		"--header=Press ENTER to install, ESC to cancel, TAB to toggle preview",
		"--bind=ctrl-r:reload(echo 'Refreshing...')",
		"--bind=tab:toggle-preview",
	}

	if previewColors {
		fzfArgs = append(fzfArgs,
			"--color=bg+:#313244,bg:#1e1e2e,spinner:#f5e0dc,hl:#f38ba8",
			"--color=fg:#cdd6f4,header:#f38ba8,info:#cba6ac,pointer:#f5e0dc",
			"--color=marker:#f5e0dc,fg+:#cdd6f4,prompt:#cba6ac,hl+:#f38ba8",
			"--ansi",
		)
	}

	cmd := exec.Command("fzf", fzfArgs...)

	cmd.Stdin = strings.NewReader(strings.Join(packages, "\n"))

	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() == 130 {
				return "", nil
			}
		}
		return "", fmt.Errorf("fzf failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func createPreviewScript(scriptPath, infoCacheDir string, useColors bool) error {
	var script string
	if useColors {
		script = fmt.Sprintf(`#!/bin/bash
PACKAGE="$1"
CACHE_FILE="%s/${PACKAGE}.txt"
USE_COLORS=true

colorize_brew_info() {
    sed -E $'s/^(==>.*)/\033[1;36m\\1\033[0m/g' | \
    sed -E $'s/(https?:\/\/[^ ]+)/\033[4;34m\\1\033[0m/g' | \
    sed -E $'s/^(Installed)$/\033[1;32m\\1\033[0m/g' | \
    sed -E $'s/^(Not installed)$/\033[1;31m\\1\033[0m/g' | \
    sed -E $'s/^(Required:|Optional:|Build:|Test:|Recommended:)/\033[1;33m\\1\033[0m/g' | \
    sed -E $'s/^(From:|License:)/\033[1;37m\\1\033[0m/g' | \
    sed -E $'s/( \\*)/\033[1;32m\\1\033[0m/g' | \
    sed -E $'s/^([^:=\\/]+): (.*)$/\033[1;37m\\1:\033[0m \\2/g'
}

if [ -f "$CACHE_FILE" ] && [ $(($(date +%%s) - $(stat -f %%m "$CACHE_FILE" 2>/dev/null || echo 0))) -lt 3600 ]; then
    cat "$CACHE_FILE" | colorize_brew_info
else
    if NO_COLOR=1 TERM=dumb brew info "$PACKAGE" 2>/dev/null > "$CACHE_FILE"; then
        cat "$CACHE_FILE" | colorize_brew_info
    else
        echo -e "\033[1;31mPackage info not available\033[0m"
    fi
fi
`, infoCacheDir)
	} else {
		script = fmt.Sprintf(`#!/bin/bash
PACKAGE="$1"
CACHE_FILE="%s/${PACKAGE}.txt"

if [ -f "$CACHE_FILE" ] && [ $(($(date +%%s) - $(stat -f %%m "$CACHE_FILE" 2>/dev/null || echo 0))) -lt 3600 ]; then
    cat "$CACHE_FILE"
else
    NO_COLOR=1 TERM=dumb brew info "$PACKAGE" 2>/dev/null | tee "$CACHE_FILE" 2>/dev/null || echo "Package info not available"
fi
`, infoCacheDir)
	}

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	return nil
}

func installPackage(packageName string, dryRun bool) error {
	if dryRun {
		fmt.Printf("Would install: %s\n", packageName)
		return nil
	}

	fmt.Printf("Installing %s...\n", packageName)

	cmd := exec.Command("brew", "install", packageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
