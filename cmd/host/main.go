package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MeguruMacabre/MeguruPacks/internal/appconfig"
	"github.com/MeguruMacabre/MeguruPacks/internal/packs"
	"github.com/MeguruMacabre/MeguruPacks/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg, err := appconfig.Load()
	if err != nil {
		fmt.Println("Ошибка загрузки встроенного конфига:", err)
		os.Exit(1)
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Не удалось определить путь к бинарнику:", err)
		os.Exit(1)
	}

	scanRoot := filepath.Dir(exePath)

	packsList, err := packs.ScanRoot(scanRoot)
	if err != nil {
		fmt.Println("Ошибка сканирования папки с инстансами:", err)
		os.Exit(1)
	}

	program := tea.NewProgram(
		tui.New(cfg, scanRoot, packsList),
		tea.WithAltScreen(),
	)

	if _, err := program.Run(); err != nil {
		fmt.Println("Ошибка запуска интерфейса:", err)
		os.Exit(1)
	}
}
