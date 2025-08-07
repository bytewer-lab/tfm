# TFM - Terminal File Manager

TFM is a Ranger-like terminal file manager written in Go using the Bubble Tea framework. It provides a three-column view with file previews and Vim-style navigation.

## Features

- Three-column layout (parent/current/preview)
- Vim-style navigation
- File operations (copy, cut, paste, delete)
- File search
- Markdown preview with syntax highlighting
- Which-key style help system

## Installation

```bash
go install github.com/bytewer-lab/tfm@latest
```

## Usage

```bash
tfm browse [path]
```

### Key Bindings

Navigation:
- `h`, `left` - Go to parent directory
- `j`, `down` - Move cursor down
- `k`, `up` - Move cursor up
- `l`, `right`, `enter` - Enter directory/Open file

File Operations:
- `dd` - Cut file
- `dD` - Delete file
- `yy` - Copy file
- `pp` - Paste file
- `gg` - Go to first file
- `G` - Go to last file

Other:
- `/` - Search
- `?` - Show/hide help
- `q` - Quit

## Building from Source

```bash
git clone https://github.com/bytewer-lab/tfm.git
cd tfm
go build
```

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Glamour](https://github.com/charmbracelet/glamour) - Markdown rendering
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Style definitions
- [Cobra](https://github.com/spf13/cobra) - CLI commands
- [Viper](https://github.com/spf13/viper) - Configuration