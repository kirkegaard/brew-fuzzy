# Brew Fuzzy Install

Fast TUI for fuzzy finding and installing Homebrew packages.

## Usage

```bash
brew-fuzzy                  # Launch fuzzy finder
brew-fuzzy --preview-colors # Launch with colorized preview
brew-fuzzy --refresh        # Refresh package cache manually
brew-fuzzy --dry-run        # Dry run (don't install)
brew-fuzzy --help           # Show help message
```

## Controls

- **Type**: Search packages (fuzzy matching)
- **↑/↓**: Navigate results  
- **Tab**: Toggle preview pane
- **Enter**: Install selected package
- **Escape**: Cancel and exit

## Installation

Install directly from GitHub:

```bash
go install github.com/kirkegaard/brew-fuzzy@latest
```

Or build from source:

```bash
go build -o brew-fuzzy
```

Cache stored in `~/.cache/brew-fuzzy/`
