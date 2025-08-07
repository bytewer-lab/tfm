# AI Agent Instructions for TFM

TFM (Terminal File Manager) is a Ranger-like file manager written in Go using Bubble Tea. Here's what you need to know to work effectively with this codebase:

## Project Structure

```
.
└── cmd/           # Core command implementations
    └── browse.go  # Main file manager implementation
```

## Key Components

1. **Command Structure** (`cmd/`)
   - Uses Cobra framework for CLI commands
   - Main command: `browse`
   - Uses Bubble Tea for terminal UI
   - Uses Glamour for Markdown rendering

2. **File Manager Features**
   - Three-column layout (parent/current/preview)
   - File/directory navigation
   - File operations (copy, cut, paste, delete)
   - Markdown preview with syntax highlighting
   - Which-key style help system

## Core Workflows

### File Navigation
- Vim-style navigation (h,j,k,l)
- Directory browsing
- File previews
- Search functionality

### File Operations
```
dd - Cut file
dD - Delete file
yy - Copy file
pp - Paste file
gg - Go to first file
G  - Go to last file
/  - Search
?  - Show/hide help
```

## Development Patterns

1. **UI Components**
   - Uses Bubble Tea for terminal UI
   - Lipgloss for styling
   - Table component for aligned help display
   - Dynamic layout calculations

2. **Error Handling**
   - Safe file operations
   - Non-blocking previews
   - Graceful fallbacks for binary files

3. **Code Organization**
   - Model-View pattern (Bubble Tea)
   - Modular preview renderers
   - Separated styling definitions

## Configuration

- Uses Viper for config management
- Customizable styles and colors
- Adaptive terminal sizing

## Common Tasks

1. **Adding New Features**
   - Add new methods to FileManager struct
   - Update key bindings in Update method
   - Add new styles if needed

2. **Testing**
   ```bash
   go run main.go browse [path]
   ```

3. **Building**
   ```bash
   go build
   ```
