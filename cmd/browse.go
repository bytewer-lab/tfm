package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// FileEntry represents a file or directory
type FileEntry struct {
	Name     string
	Path     string
	IsDir    bool
	Selected bool
}

// UndoAction represents an action that can be undone
type UndoAction struct {
	Type    string    // "delete", "move", "rename"
	OldPath string    // Original path
	NewPath string    // New path (for moves/renames)
	Entry   FileEntry // File information
	OldName string    // Original name (for renames)
}

// FileManager represents the application state
type FileManager struct {
	CurrentPath string
	Entries     []FileEntry
	Cursor      int
	Width       int
	Height      int

	// State for shortcuts
	clipboard    *FileEntry // Clipboard entry
	clipboardOp  string     // Clipboard operation: "copy" or "cut"
	searchMode   bool       // Search mode active
	searchQuery  string     // Current search text
	renameMode   bool       // Rename mode active
	renameText   string     // Current rename text
	zoxideMode   bool       // Zoxide mode active
	zoxideQuery  string     // Current zoxide query
	lastCommand  string     // Last command (for double commands like dd)
	commandTime  time.Time  // Time of last command
	showWhichKey bool       // Show shortcuts screen

	// Undo system
	undoStack []UndoAction // Stack of actions to undo
	trashDir  string       // Temporary directory for trash
}

// Structure to define a shortcut
type shortcut struct {
	key         string
	description string
}

// Map of shortcut contexts
var shortcuts = map[string][]shortcut{
	"normal": {
		{"dd", "cut file"},
		{"dD or DD", "delete file"},
		{"yy", "copy file"},
		{"pp", "paste file"},
		{"u", "undo"},
		{"a", "rename file"},
		{"/", "search"},
		{"z", "navigate with zoxide"},
		{"gg", "go to first"},
		{"G", "go to last"},
		{"S", "open terminal"},
		{"?", "show/hide shortcuts"},
		{"q", "quit"},
		{"l, enter", "open file"},
	},
	"search": {
		{"enter", "confirm search"},
		{"esc", "cancel search"},
	},
	"rename": {
		{"enter", "confirm rename"},
		{"esc", "cancel rename"},
	},
	"zoxide": {
		{"enter", "navigate to directory"},
		{"esc", "cancel navigation"},
	},
}

const (
	contentLimit   = 10 // Limit of items in directory
	emptyDirMsg    = "Empty directory"
	noSelectionMsg = "No item selected"
)

// Markdown renderer
var markdownRenderer *glamour.TermRenderer

func init() {
	// Initialize renderer with default configuration
	markdownRenderer, _ = glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(-1), // Disable automatic wrap
	)
}

// Style for columns
var (
	columnStyle = lipgloss.NewStyle().
			PaddingLeft(2).
			PaddingRight(2)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	dirStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	pathStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			PaddingLeft(2).
			PaddingBottom(1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("234")).
			Background(lipgloss.Color("252")).
			PaddingLeft(2).
			PaddingTop(0).
			PaddingBottom(0)

	searchBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("234")).
			Background(lipgloss.Color("255")).
			PaddingLeft(2).
			PaddingTop(0).
			PaddingBottom(0)

	whichKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("234")).
			Background(lipgloss.Color("252")).
			PaddingLeft(2).
			PaddingRight(2)

	emptyStateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true)
)

// Reads files from the current directory
// ReadDirectory reads files from a directory
func ReadDirectory(path string) []FileEntry {
	var entries []FileEntry
	files, _ := os.ReadDir(path)

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), ".") { // Ignore hidden files
			entries = append(entries, FileEntry{
				Name:  file.Name(),
				Path:  filepath.Join(path, file.Name()),
				IsDir: file.IsDir(),
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})

	return entries
}

func (m *FileManager) Init() tea.Cmd {
	return tea.EnterAltScreen
}

// tryEnterDirectory tries to enter the selected directory or opens the file
func (m *FileManager) tryEnterDirectory() {
	if m.Cursor < len(m.Entries) {
		entry := m.Entries[m.Cursor]
		if entry.IsDir {
			m.CurrentPath = entry.Path
			m.Entries = ReadDirectory(entry.Path)
			m.Cursor = 0
		} else {
			// If it's a file, open with default program
			if err := openWithDefaultApp(entry.Path); err != nil {
				// In case of error, you might want to log or show in interface
				// For now, we just ignore the error
			}
		}
	}
}

func (m *FileManager) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case reloadDirectoryMsg:
		// Reload directory after returning from terminal
		m.Entries = ReadDirectory(m.CurrentPath)
		return m, nil
	case tea.KeyMsg:
		// If in search mode
		if m.searchMode {
			switch msg.Type {
			case tea.KeyEnter:
				m.searchMode = false
				m.searchFiles(m.searchQuery)
				m.searchQuery = ""
			case tea.KeyEsc:
				m.searchMode = false
				m.searchQuery = ""
			case tea.KeyBackspace:
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				}
			default:
				m.searchQuery += msg.String()
			}
			return m, nil
		}

		// If in rename mode
		if m.renameMode {
			switch msg.Type {
			case tea.KeyEnter:
				m.renameMode = false
				m.renameFile(m.renameText)
				m.renameText = ""
			case tea.KeyEsc:
				m.renameMode = false
				m.renameText = ""
			case tea.KeyBackspace:
				if len(m.renameText) > 0 {
					m.renameText = m.renameText[:len(m.renameText)-1]
				}
			default:
				m.renameText += msg.String()
			}
			return m, nil
		}

		// If in zoxide mode
		if m.zoxideMode {
			switch msg.Type {
			case tea.KeyEnter:
				m.zoxideMode = false
				m.navigateWithZoxide(m.zoxideQuery)
				m.zoxideQuery = ""
			case tea.KeyEsc:
				m.zoxideMode = false
				m.zoxideQuery = ""
			case tea.KeyBackspace:
				if len(m.zoxideQuery) > 0 {
					m.zoxideQuery = m.zoxideQuery[:len(m.zoxideQuery)-1]
				}
			default:
				m.zoxideQuery += msg.String()
			}
			return m, nil
		}

		// Normal mode
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case "down", "j":
			if m.Cursor < len(m.Entries)-1 {
				m.Cursor++
			}
		case "l", "enter", "right":
			m.tryEnterDirectory()
		case "h", "left":
			// Go back to parent directory
			parent := filepath.Dir(m.CurrentPath)
			if parent != m.CurrentPath {
				currentDir := filepath.Base(m.CurrentPath)
				m.CurrentPath = parent
				m.Entries = ReadDirectory(parent)

				// Search and select current directory in list
				for i, entry := range m.Entries {
					if entry.Name == currentDir {
						m.Cursor = i
						break
					}
				}
			}
		case "d":
			if m.handleDoubleCommand("d") {
				m.cutFile()
			}
		case "D":
			// If last command was "d", then it's dD (delete)
			if m.lastCommand == "d" && time.Since(m.commandTime) < 500*time.Millisecond {
				m.deleteFile()
				m.lastCommand = ""
			} else if m.handleDoubleCommand("D") {
				// DD also deletes (alternative command)
				m.deleteFile()
			}
		case "y":
			if m.handleDoubleCommand("y") {
				m.copyFile()
			}
		case "p":
			if m.handleDoubleCommand("p") {
				m.pasteFile()
			}
		case "/":
			m.searchMode = true
			m.searchQuery = ""
		case "a":
			if len(m.Entries) > 0 && m.Cursor < len(m.Entries) {
				m.renameMode = true
				m.renameText = m.Entries[m.Cursor].Name
			}
		case "z":
			m.zoxideMode = true
			m.zoxideQuery = ""
		case "g":
			if m.handleDoubleCommand("g") {
				m.Cursor = 0
			}
		case "G":
			m.Cursor = len(m.Entries) - 1
		case "u":
			m.undoLastAction()
		case "S":
			// Shift+S: Open terminal in current directory
			return m, m.openTerminal()
		case "?":
			m.showWhichKey = !m.showWhichKey
		}
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
	}
	return m, nil
}

// renderParentColumn renders the parent directory column
func (m *FileManager) renderParentColumn(colWidth int) string {
	parent := filepath.Dir(m.CurrentPath)
	if parent == m.CurrentPath {
		return columnStyle.Width(colWidth).Render("System root")
	}

	var parentCol strings.Builder
	parentEntries := ReadDirectory(parent)
	currentBase := filepath.Base(m.CurrentPath)

	for _, entry := range parentEntries {
		line := entry.Name
		if entry.IsDir {
			line = dirStyle.Render(line + "/")
		}
		if entry.Name == currentBase {
			line = selectedStyle.Render("> " + line)
		} else {
			line = "  " + line
		}
		parentCol.WriteString(line + "\n")
	}

	return columnStyle.Width(colWidth).Render(parentCol.String())
}

// renderDirPreview renders the preview of a directory
func renderDirPreview(path string) string {
	var preview strings.Builder
	entries := ReadDirectory(path)

	if len(entries) == 0 {
		return emptyDirMsg
	}

	for i, entry := range entries {
		if i > contentLimit {
			preview.WriteString("...\n")
			break
		}

		line := entry.Name
		if entry.IsDir {
			line = dirStyle.Render(line + "/")
		}
		preview.WriteString("  " + line + "\n")
	}
	return preview.String()
}

// renderMarkdownPreview renders a markdown file
func renderMarkdownPreview(content []byte, maxHeight int) string {
	rendered, err := markdownRenderer.Render(string(content))
	if err != nil {
		return "Error rendering markdown"
	}

	// Limit number of lines to maximum height
	lines := strings.Split(rendered, "\n")
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
		lines = append(lines, "...")
	}
	return strings.Join(lines, "\n")
}

// renderTextPreview renders a text file
func renderTextPreview(content []byte, colWidth, maxHeight int) string {
	var preview strings.Builder
	lines := strings.Split(string(content), "\n")

	// Limit number of lines to maximum height
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
		lines = append(lines, "...")
	}

	for _, line := range lines {
		if len(line) > colWidth-4 {
			line = line[:colWidth-7] + "..."
		}
		preview.WriteString(line + "\n")
	}

	return preview.String()
}

// renderFilePreview renders the preview of a file
func renderFilePreview(file FileEntry, colWidth, maxHeight int) string {
	content, err := os.ReadFile(file.Path)
	if err != nil {
		return "Error reading file"
	}

	// If it's a markdown file, use glamour
	if strings.HasSuffix(strings.ToLower(file.Name), ".md") {
		return renderMarkdownPreview(content, maxHeight)
	}

	// For other text files
	if len(content) > 0 && !containsNullByte(content) {
		return renderTextPreview(content, colWidth, maxHeight)
	}

	// For binary files
	return "[Binary file]"
}

// renderPreviewColumn renders the preview column
func (m *FileManager) renderPreviewColumn(colWidth int) string {
	if len(m.Entries) == 0 {
		return columnStyle.Width(colWidth).Render(emptyDirMsg)
	}
	if m.Cursor >= len(m.Entries) {
		return columnStyle.Width(colWidth).Render(noSelectionMsg)
	}

	selected := m.Entries[m.Cursor]
	var content string

	// Calculate available height for preview
	headerHeight := 2 // 1 content line + 1 padding
	statusHeight := 1 // 1 content line
	whichKeyHeight := 0
	if m.showWhichKey {
		if m.searchMode {
			whichKeyHeight = 3
		} else {
			whichKeyHeight = 5
		}
	}
	maxPreviewHeight := m.Height - headerHeight - statusHeight - whichKeyHeight - 2 // -2 for margins

	if selected.IsDir {
		content = renderDirPreview(selected.Path)
	} else {
		content = renderFilePreview(selected, colWidth, maxPreviewHeight)
	}

	return columnStyle.Width(colWidth).Render(content)
}

// getFileInfo returns detailed file information
func getFileInfo(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "Error getting file information"
	}

	// Get user and group information
	stat := info.Sys().(*syscall.Stat_t)
	uid := stat.Uid
	gid := stat.Gid

	// Convert UID and GID to names
	u, err := user.LookupId(fmt.Sprint(uid))
	owner := fmt.Sprint(uid)
	if err == nil {
		owner = u.Username
	}

	g, err := user.LookupGroupId(fmt.Sprint(gid))
	group := fmt.Sprint(gid)
	if err == nil {
		group = g.Name
	}

	// Format permissions
	mode := info.Mode().String()

	// Format size
	size := ""
	if info.IsDir() {
		items, _ := os.ReadDir(path)
		size = fmt.Sprintf("%d items", len(items))
	} else {
		bytes := info.Size()
		switch {
		case bytes < 1024:
			size = fmt.Sprintf("%dB", bytes)
		case bytes < 1024*1024:
			size = fmt.Sprintf("%.1fK", float64(bytes)/1024)
		case bytes < 1024*1024*1024:
			size = fmt.Sprintf("%.1fM", float64(bytes)/1024/1024)
		default:
			size = fmt.Sprintf("%.1fG", float64(bytes)/1024/1024/1024)
		}
	}

	// Format modification date
	modTime := info.ModTime().Format("02 Jan 2006 15:04")

	return fmt.Sprintf("%s  %s  %s  %s  %s", mode, owner, group, size, modTime)
}

// openWithDefaultApp opens a file with the system's default program
func openWithDefaultApp(path string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	default: // Linux and others
		cmd = exec.Command("xdg-open", path)
	}

	return cmd.Start()
}

// containsNullByte checks if content appears to be binary
func containsNullByte(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// Methods for file manipulation
func (m *FileManager) cutFile() {
	if len(m.Entries) > 0 && m.Cursor < len(m.Entries) {
		entry := m.Entries[m.Cursor]
		m.clipboard = &entry
		m.clipboardOp = "cut"

		// Add to undo stack to restore visually if necessary
		undoAction := UndoAction{
			Type:    "cut",
			OldPath: entry.Path,
			Entry:   entry,
		}
		m.undoStack = append(m.undoStack, undoAction)

		// Remove file from visual list (will be moved when pasted)
		m.Entries = append(m.Entries[:m.Cursor], m.Entries[m.Cursor+1:]...)
		if m.Cursor >= len(m.Entries) && len(m.Entries) > 0 {
			m.Cursor = len(m.Entries) - 1
		} else if len(m.Entries) == 0 {
			m.Cursor = 0
		}
	}
}

func (m *FileManager) deleteFile() {
	if len(m.Entries) > 0 && m.Cursor < len(m.Entries) {
		entry := m.Entries[m.Cursor]

		// Create trash directory if it doesn't exist
		if m.trashDir == "" {
			tmpDir, err := os.MkdirTemp("", "tfm_trash_")
			if err != nil {
				return // Silent failure if unable to create directory
			}
			m.trashDir = tmpDir
		}

		// Move file to trash instead of permanently deleting it
		trashPath := filepath.Join(m.trashDir, entry.Name)

		// If a file with the same name already exists in trash, add a suffix
		counter := 1
		originalTrashPath := trashPath
		for {
			if _, err := os.Stat(trashPath); os.IsNotExist(err) {
				break
			}
			ext := filepath.Ext(originalTrashPath)
			name := strings.TrimSuffix(originalTrashPath, ext)
			trashPath = fmt.Sprintf("%s_%d%s", name, counter, ext)
			counter++
		}

		if err := os.Rename(entry.Path, trashPath); err == nil {
			// Add to undo stack
			undoAction := UndoAction{
				Type:    "delete",
				OldPath: entry.Path,
				NewPath: trashPath, // Save where it is in trash
				Entry:   entry,
			}
			m.undoStack = append(m.undoStack, undoAction)

			// Update list
			m.Entries = ReadDirectory(m.CurrentPath)
			if m.Cursor >= len(m.Entries) && len(m.Entries) > 0 {
				m.Cursor = len(m.Entries) - 1
			} else if len(m.Entries) == 0 {
				m.Cursor = 0
			}
		}
	}
}

func (m *FileManager) copyFile() {
	if len(m.Entries) > 0 && m.Cursor < len(m.Entries) {
		entry := m.Entries[m.Cursor]
		m.clipboard = &entry
		m.clipboardOp = "copy"
	}
}

func (m *FileManager) pasteFile() {
	if m.clipboard == nil {
		return
	}

	destPath := filepath.Join(m.CurrentPath, m.clipboard.Name)

	var err error
	if m.clipboardOp == "cut" {
		// For cut, check if we are in the same directory
		clipboardDir := filepath.Dir(m.clipboard.Path)
		if clipboardDir == m.CurrentPath {
			// If we are in the same directory, just restore the file in the list
			// (undo the visual cut)
			m.clipboard = nil
			m.clipboardOp = ""
			// Reload the list to show the file again
			m.Entries = ReadDirectory(m.CurrentPath)
			return
		}

		// If we are in a different directory, move the file
		if _, statErr := os.Stat(m.clipboard.Path); statErr == nil {
			// Move the file
			err = os.Rename(m.clipboard.Path, destPath)
			if err == nil {
				// Add to undo stack for the movement
				undoAction := UndoAction{
					Type:    "move",
					OldPath: m.clipboard.Path,
					NewPath: destPath,
					Entry:   *m.clipboard,
				}
				m.undoStack = append(m.undoStack, undoAction)
			}
		} else {
			// The file no longer exists
			err = statErr
		}
		// Clear clipboard after cut+paste
		m.clipboard = nil
		m.clipboardOp = ""
	} else if m.clipboardOp == "copy" {
		// For copy, check if a file with the same name already exists and add suffix
		if _, statErr := os.Stat(destPath); statErr == nil {
			// If it exists, add a suffix
			ext := filepath.Ext(m.clipboard.Name)
			name := strings.TrimSuffix(m.clipboard.Name, ext)
			destPath = filepath.Join(m.CurrentPath, name+"_copy"+ext)
		}

		// Copy the file
		err = copyFileOrDir(m.clipboard.Path, destPath)
		if err == nil {
			// Add to undo stack for the copy
			undoAction := UndoAction{
				Type:    "copy",
				OldPath: "",       // No original location to restore
				NewPath: destPath, // File that was created
				Entry:   *m.clipboard,
			}
			m.undoStack = append(m.undoStack, undoAction)
		}
		// For copy, don't clear clipboard to allow multiple copies
	}

	if err == nil {
		// Update list
		m.Entries = ReadDirectory(m.CurrentPath)
	}
}

// copyFileOrDir copies a file or directory recursively
func copyFileOrDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

// copyFile copies a single file
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = srcFile.WriteTo(dstFile)
	return err
}

// copyDir copies a directory recursively
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dst, srcInfo.Mode())
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if err := copyFileOrDir(srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// undoLastAction undoes the last action
func (m *FileManager) undoLastAction() {
	if len(m.undoStack) == 0 {
		return
	}

	// Get the last action
	lastAction := m.undoStack[len(m.undoStack)-1]
	m.undoStack = m.undoStack[:len(m.undoStack)-1]

	switch lastAction.Type {
	case "delete":
		// Restore file from trash to original location
		if lastAction.NewPath != "" {
			if err := os.Rename(lastAction.NewPath, lastAction.OldPath); err != nil {
				// If it fails, put back in undo stack
				m.undoStack = append(m.undoStack, lastAction)
				return
			}
		}
	case "cut":
		// Restore file in visual list (cancel the cut)
		// Reinsert file in original position
		m.clipboard = nil
		m.clipboardOp = ""
	case "copy":
		// Remove the file that was copied
		if lastAction.NewPath != "" {
			os.RemoveAll(lastAction.NewPath)
		}
	case "move":
		// Undo a movement (cut+paste)
		if err := os.Rename(lastAction.NewPath, lastAction.OldPath); err != nil {
			// If it fails, put back in undo stack
			m.undoStack = append(m.undoStack, lastAction)
			return
		}
	case "rename":
		// Undo a rename
		if err := os.Rename(lastAction.NewPath, lastAction.OldPath); err != nil {
			// If it fails, put back in undo stack
			m.undoStack = append(m.undoStack, lastAction)
			return
		}
	}

	// Update list
	m.Entries = ReadDirectory(m.CurrentPath)
}

// cleanupTrash cleans up the temporary trash directory
func (m *FileManager) cleanupTrash() {
	if m.trashDir != "" {
		os.RemoveAll(m.trashDir)
	}
}

func (m *FileManager) searchFiles(query string) {
	if query == "" {
		return
	}
	query = strings.ToLower(query)
	for i, entry := range m.Entries {
		if strings.Contains(strings.ToLower(entry.Name), query) {
			m.Cursor = i
			return
		}
	}
}

func (m *FileManager) renameFile(newName string) {
	if newName == "" || len(m.Entries) == 0 || m.Cursor >= len(m.Entries) {
		return
	}

	entry := m.Entries[m.Cursor]
	newPath := filepath.Join(m.CurrentPath, newName)

	// Only rename if the name is different
	if newName != entry.Name {
		if err := os.Rename(entry.Path, newPath); err == nil {
			// Add to undo stack
			undoAction := UndoAction{
				Type:    "rename",
				OldPath: entry.Path,
				NewPath: newPath,
				Entry:   entry,
				OldName: entry.Name,
			}
			m.undoStack = append(m.undoStack, undoAction)

			// Reload list to maintain sorting
			m.Entries = ReadDirectory(m.CurrentPath)

			// Find new position of renamed file
			for i, e := range m.Entries {
				if e.Name == newName {
					m.Cursor = i
					break
				}
			}
		}
	}
}

func (m *FileManager) navigateWithZoxide(query string) {
	if query == "" {
		return
	}

	// Execute zoxide command to find directory
	cmd := exec.Command("zoxide", "query", query)
	output, err := cmd.Output()
	if err != nil {
		return // Silent failure if zoxide is not available or doesn't find directory
	}

	targetPath := strings.TrimSpace(string(output))
	if targetPath == "" {
		return
	}

	// Check if directory exists
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		m.CurrentPath = targetPath
		m.Entries = ReadDirectory(targetPath)
		m.Cursor = 0
	}
}

// openTerminal opens a terminal in current directory and suspends the TUI
func (m *FileManager) openTerminal() tea.Cmd {
	// Determine which shell to use
	shell := os.Getenv("SHELL")
	if shell == "" {
		switch runtime.GOOS {
		case "windows":
			shell = "cmd"
		default:
			shell = "/bin/bash"
		}
	}

	// Create terminal command
	cmd := exec.Command(shell)
	cmd.Dir = m.CurrentPath

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		// Callback executed after shell terminates
		// Reload current directory (there may have been changes)
		return reloadDirectoryMsg{}
	})
}

// Custom message to reload directory
type reloadDirectoryMsg struct{}

// renderWhichKey renders the shortcuts screen
func (m *FileManager) renderWhichKey() string {
	if !m.showWhichKey {
		return ""
	}

	// Determine which set of shortcuts to show
	var currentShortcuts []shortcut
	if m.searchMode {
		currentShortcuts = shortcuts["search"]
	} else if m.renameMode {
		currentShortcuts = shortcuts["rename"]
	} else if m.zoxideMode {
		currentShortcuts = shortcuts["zoxide"]
	} else {
		currentShortcuts = shortcuts["normal"]
	}

	// Prepare data for table
	rows := make([]table.Row, 0, len(currentShortcuts))
	for _, s := range currentShortcuts {
		rows = append(rows, table.Row{s.key, s.description})
	}

	// Configure table
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "", Width: 10},
			{Title: "", Width: 20},
		}),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(len(rows)),
	)

	// Style the table
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderBottom(false).
		Bold(false).
		Foreground(lipgloss.Color("234")).
		Background(lipgloss.Color("252"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("205")).
		Background(lipgloss.Color("252")).
		Bold(true)
	s.Cell = s.Cell.
		Foreground(lipgloss.Color("234")).
		Background(lipgloss.Color("252"))

	t.SetStyles(s)

	// Render within which-key style
	return whichKeyStyle.
		Width(m.Width).
		Render(t.View())
}

func (m *FileManager) handleDoubleCommand(cmd string) bool {
	now := time.Now()
	if cmd == m.lastCommand && now.Sub(m.commandTime) < 500*time.Millisecond {
		m.lastCommand = ""
		return true
	}
	m.lastCommand = cmd
	m.commandTime = now
	return false
}

func (m *FileManager) View() string {
	// 1. Height calculations - which-key doesn't affect main layout
	headerHeight := 1  // Path height
	statusHeight := 1  // Status bar height
	commandHeight := 1 // Command/search line height
	// whichKeyHeight doesn't factor into availableHeight calculation
	availableHeight := m.Height - headerHeight - statusHeight - commandHeight - 1 // -1 for content margin top

	// 2. Width calculations
	contentWidth := m.Width - 4
	leftColWidth := contentWidth * 20 / 100  // 20% for left column
	mainColWidth := contentWidth * 30 / 100  // 30% for center column
	rightColWidth := contentWidth * 50 / 100 // 50% for right column

	// 3. Calculate number of visible items
	visibleCount := availableHeight // Use all available height

	// 4. Build layout using strings.Builder
	var view strings.Builder

	// 5. Add header
	headerStyle := pathStyle.
		Width(m.Width).
		MarginBottom(1)
	view.WriteString(headerStyle.Render(m.CurrentPath))

	// 6. Render current column
	var currentCol strings.Builder
	if len(m.Entries) == 0 {
		currentCol.WriteString(lipgloss.JoinVertical(lipgloss.Left,
			emptyDirMsg,
			"",
			emptyStateStyle.Render("Use h to go back to parent directory"),
		))
	} else {
		startIdx := max(0, m.Cursor-visibleCount/2)
		endIdx := min(len(m.Entries), startIdx+visibleCount)

		for i := startIdx; i < endIdx; i++ {
			entry := m.Entries[i]
			line := entry.Name
			if entry.IsDir {
				line = dirStyle.Render(line + "/")
			}
			if i == m.Cursor {
				line = selectedStyle.Render("> " + line)
			} else {
				line = "  " + line
			}
			currentCol.WriteString(line + "\n")
		}
	}

	// 7. Combine columns with limited height
	columns := lipgloss.JoinHorizontal(
		lipgloss.Left,
		m.renderParentColumn(leftColWidth),
		columnStyle.Width(mainColWidth).Render(currentCol.String()),
		m.renderPreviewColumn(rightColWidth),
	)

	// 8. Add main content with padding
	mainStyle := lipgloss.NewStyle().
		MaxHeight(availableHeight).
		Height(availableHeight).
		MarginTop(1)

	view.WriteString(mainStyle.Render(columns))

	// 9. Prepare status bar (always present)
	var status string
	if len(m.Entries) > 0 && m.Cursor < len(m.Entries) {
		selected := m.Entries[m.Cursor]
		status = getFileInfo(selected.Path)
	} else {
		status = noSelectionMsg
	}

	// 10. Render status bar
	view.WriteString("\n")
	finalStatusStyle := statusStyle.Width(m.Width)
	view.WriteString(finalStatusStyle.Render(status))

	// 11. Prepare and render command/search/rename/zoxide line
	view.WriteString("\n")
	if m.searchMode {
		// Search mode: show search bar
		searchPrompt := fmt.Sprintf("Search: %s█", m.searchQuery)
		finalSearchBarStyle := searchBarStyle.Width(m.Width)
		view.WriteString(finalSearchBarStyle.Render(searchPrompt))
	} else if m.renameMode {
		// Rename mode: show rename bar
		renamePrompt := fmt.Sprintf("Rename: %s█", m.renameText)
		finalRenameBarStyle := searchBarStyle.Width(m.Width)
		view.WriteString(finalRenameBarStyle.Render(renamePrompt))
	} else if m.zoxideMode {
		// Zoxide mode: show zoxide bar
		zoxidePrompt := fmt.Sprintf("z %s█", m.zoxideQuery)
		finalZoxideBarStyle := searchBarStyle.Width(m.Width)
		view.WriteString(finalZoxideBarStyle.Render(zoxidePrompt))
	} else {
		// Normal mode: blank or empty line
		emptyCommandStyle := lipgloss.NewStyle().Width(m.Width)
		view.WriteString(emptyCommandStyle.Render(""))
	}

	// 12. If which-key active, overlay on content area (doesn't add height)
	if m.showWhichKey {
		baseView := view.String()
		baseLines := strings.Split(baseView, "\n")

		whichKeyContent := m.renderWhichKey()
		whichKeyLines := strings.Split(whichKeyContent, "\n")

		// Calculate where to insert which-key (above the two bottom bars)
		bottomBarsCount := 2 // status bar + command line
		insertPos := len(baseLines) - bottomBarsCount - len(whichKeyLines)
		if insertPos < headerHeight+1 {
			insertPos = headerHeight + 1
		}

		// Replace lines at calculated position
		for i, wkLine := range whichKeyLines {
			if insertPos+i < len(baseLines)-bottomBarsCount {
				baseLines[insertPos+i] = wkLine
			}
		}

		return strings.Join(baseLines, "\n")
	}

	return view.String()
}

// browse command
var browseCmd = &cobra.Command{
	Use:   "browse [path]",
	Short: "Open the TFM file manager (TUI)",
	Run: func(cmd *cobra.Command, args []string) {
		// Load configurations with Viper
		viper.SetConfigName("tfm")
		viper.AddConfigPath("$HOME/.config/tfm/")
		_ = viper.ReadInConfig() // ignore error if doesn't exist

		// Define initial directory
		startPath := "."
		if len(args) > 0 {
			startPath = args[0]
		}

		// Convert to absolute path
		absPath, err := filepath.Abs(startPath)
		if err != nil {
			fmt.Println("Error resolving path:", err)
			os.Exit(1)
		}

		// Check if directory exists
		info, err := os.Stat(absPath)
		if err != nil {
			fmt.Println("Error accessing directory:", err)
			os.Exit(1)
		}
		if !info.IsDir() {
			fmt.Println("The specified path is not a directory")
			os.Exit(1)
		}

		// Initialize model with directory
		initialModel := &FileManager{
			CurrentPath: absPath,
			Entries:     ReadDirectory(absPath),
			Cursor:      0,
		}

		p := tea.NewProgram(initialModel, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Println("Error starting TUI:", err)
			os.Exit(1)
		}

		// Clean up temporary trash when exiting
		initialModel.cleanupTrash()
	},
}

func init() {
	rootCmd.AddCommand(browseCmd)
}
