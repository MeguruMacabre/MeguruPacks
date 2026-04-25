package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/appconfig"
	gcsvc "github.com/MeguruMacabre/MeguruPacks/internal/gc"
	"github.com/MeguruMacabre/MeguruPacks/internal/manifest"
	"github.com/MeguruMacabre/MeguruPacks/internal/packcleanup"
	"github.com/MeguruMacabre/MeguruPacks/internal/packmeta"
	"github.com/MeguruMacabre/MeguruPacks/internal/packs"
	"github.com/MeguruMacabre/MeguruPacks/internal/publicindex"
	"github.com/MeguruMacabre/MeguruPacks/internal/publish"
	"github.com/MeguruMacabre/MeguruPacks/internal/serverpacks"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
	"github.com/MeguruMacabre/MeguruPacks/internal/unpublish"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	screenList screen = iota
	screenLocalDetails
	screenServerDetails
	screenServerHistoryLoading
	screenServerHistory
	screenManifestLoading
	screenManifest
	screenProgress
	screenPublishResult
	screenDeleteConfirm
	screenDeleteLoading
	screenDeleteResult
	screenGCConfirm
	screenGCLoading
	screenGCResult
)

type pane int

const (
	paneServer pane = iota
	paneLocal
)

type versionEditMode int

const (
	versionEditNone versionEditMode = iota
	versionEditPatch
	versionEditMinor
	versionEditMajor
)

func (v versionEditMode) PublishMode() string {
	switch v {
	case versionEditPatch:
		return "patch"
	case versionEditMinor:
		return "minor"
	case versionEditMajor:
		return "major"
	case versionEditNone:
		return ""
	default:
		return ""
	}
}

type packsLoadedMsg struct {
	packs []packs.Pack
	err   error
}

type serverPacksLoadedMsg struct {
	packs []serverpacks.Pack
	err   error
}

type serverHistoryLoadedMsg struct {
	history []serverpacks.HistoryEntry
	err     error
}

type usageLoadedMsg struct {
	usedBytes int64
	err       error
}

type manifestBuiltMsg struct {
	manifest manifest.Manifest
	err      error
}

type publishDoneMsg struct {
	result publish.Result
	err    error
}

type deleteDoneMsg struct {
	result unpublish.Result
	err    error
}

type gcDoneMsg struct {
	result gcsvc.Result
	err    error
}

type progressMsg struct {
	update publish.ProgressUpdate
	ch     <-chan publish.ProgressUpdate
}

type Model struct {
	cfg  appconfig.Config
	root string

	packs         []packs.Pack
	serverPacks   []serverpacks.Pack
	serverHistory []serverpacks.HistoryEntry

	localCursor   int
	serverCursor  int
	historyOffset int
	focusedPane   pane

	versionEdit versionEditMode

	detailOffset   int
	manifestOffset int

	screen       screen
	returnScreen screen

	width  int
	height int

	err           error
	manifest      *manifest.Manifest
	publishResult *publish.Result
	deleteResult  *unpublish.Result
	gcResult      *gcsvc.Result

	s3UsedBytes     int64
	s3CapacityBytes int64
	s3UsageLoaded   bool
	s3UsageErr      error

	progressStage       string
	progressCurrentFile string
	progressFilesDone   int
	progressFilesTotal  int
	progressBytesDone   int64
	progressBytesTotal  int64
	progressPercent     float64
	progressDone        bool
	progressErr         error
}

func New(cfg appconfig.Config, root string, packsList []packs.Pack) *Model {
	return &Model{
		cfg:             cfg,
		root:            root,
		packs:           packsList,
		serverPacks:     []serverpacks.Pack{},
		serverHistory:   []serverpacks.HistoryEntry{},
		screen:          screenList,
		focusedPane:     paneLocal,
		s3CapacityBytes: cfg.S3.CapacityBytes,
		versionEdit:     versionEditNone,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		loadUsageCmd(m.cfg),
		loadServerPacksCmd(m.cfg),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case packsLoadedMsg:
		m.packs = msg.packs
		m.err = msg.err
		if m.localCursor >= len(m.packs) {
			m.localCursor = intMax(0, len(m.packs)-1)
		}
		m.detailOffset = 0
		m.manifestOffset = 0
		return m, nil

	case serverPacksLoadedMsg:
		m.serverPacks = msg.packs
		if msg.err != nil {
			m.err = msg.err
		}
		if m.serverCursor >= len(m.serverPacks) {
			m.serverCursor = intMax(0, len(m.serverPacks)-1)
		}
		return m, nil

	case serverHistoryLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.serverHistory = nil
			m.screen = screenServerDetails
			return m, nil
		}
		m.serverHistory = msg.history
		m.historyOffset = 0
		m.screen = screenServerHistory
		return m, nil

	case usageLoadedMsg:
		m.s3UsageLoaded = true
		m.s3UsageErr = msg.err
		if msg.err == nil {
			m.s3UsedBytes = msg.usedBytes
		}
		return m, nil

	case manifestBuiltMsg:
		if msg.err != nil {
			m.err = msg.err
			m.screen = screenLocalDetails
			return m, nil
		}

		m.manifest = &msg.manifest
		m.manifestOffset = 0
		m.screen = screenManifest
		return m, nil

	case progressMsg:
		return m.handleProgressMessage(msg)

	case publishDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.publishResult = nil
			m.progressErr = msg.err
		} else {
			m.publishResult = &msg.result
			m.versionEdit = versionEditNone
		}

		m.progressDone = true
		m.screen = screenProgress
		return m, tea.Batch(
			loadUsageCmd(m.cfg),
			rebuildPublicIndexCmd(m.cfg),
			loadServerPacksCmd(m.cfg),
			scanCmd(m.root),
		)

	case deleteDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.deleteResult = nil
		} else {
			m.deleteResult = &msg.result
		}
		m.screen = screenDeleteResult
		return m, tea.Batch(
			loadUsageCmd(m.cfg),
			rebuildPublicIndexCmd(m.cfg),
			loadServerPacksCmd(m.cfg),
		)

	case gcDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.gcResult = nil
		} else {
			m.gcResult = &msg.result
		}
		m.screen = screenGCResult
		return m, tea.Batch(
			loadUsageCmd(m.cfg),
			rebuildPublicIndexCmd(m.cfg),
			loadServerPacksCmd(m.cfg),
		)

	case tea.KeyMsg:
		key := msg.String()

		switch {
		case key == "ctrl+c" || isQuitKey(key):
			return m, tea.Quit
		case isRescanKey(key):
			return m, tea.Batch(
				scanCmd(m.root),
				loadUsageCmd(m.cfg),
				loadServerPacksCmd(m.cfg),
			)
		}

		switch m.screen {
		case screenList:
			return m.updateList(key)
		case screenLocalDetails:
			return m.updateLocalDetails(key)
		case screenServerDetails:
			return m.updateServerDetails(key)
		case screenServerHistoryLoading:
			return m.updateServerHistoryLoading(key)
		case screenServerHistory:
			return m.updateServerHistory(key)
		case screenManifestLoading:
			return m.updateManifestLoading(key)
		case screenManifest:
			return m.updateManifest(key)
		case screenProgress:
			return m.updateProgress(key)
		case screenPublishResult:
			return m.updatePublishResult(key)
		case screenDeleteConfirm:
			return m.updateDeleteConfirm(key)
		case screenDeleteLoading:
			return m.updateDeleteLoading(key)
		case screenDeleteResult:
			return m.updateDeleteResult(key)
		case screenGCConfirm:
			return m.updateGCConfirm(key)
		case screenGCLoading:
			return m.updateGCLoading(key)
		case screenGCResult:
			return m.updateGCResult(key)
		default:
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) updateList(key string) (tea.Model, tea.Cmd) {
	switch {
	case key == "tab":
		m.switchPane()

	case isUpKey(key):
		if m.focusedPane == paneLocal {
			if m.localCursor > 0 {
				m.localCursor--
			}
		} else {
			if m.serverCursor > 0 {
				m.serverCursor--
			}
		}

	case isDownKey(key):
		if m.focusedPane == paneLocal {
			if m.localCursor < len(m.packs)-1 {
				m.localCursor++
			}
		} else {
			if m.serverCursor < len(m.serverPacks)-1 {
				m.serverCursor++
			}
		}

	case key == "enter":
		if m.focusedPane == paneLocal {
			if len(m.packs) > 0 {
				m.screen = screenLocalDetails
				m.detailOffset = 0
				m.versionEdit = versionEditNone
			}
		} else {
			if len(m.serverPacks) > 0 {
				m.screen = screenServerDetails
			}
		}

	case isGCKey(key):
		m.gcResult = nil
		m.returnScreen = screenList
		m.screen = screenGCConfirm
	}

	return m, nil
}

func (m *Model) updateLocalDetails(key string) (tea.Model, tea.Cmd) {
	if len(m.packs) == 0 {
		m.screen = screenList
		return m, nil
	}

	pack := m.packs[m.localCursor]
	pageSize := m.filePageSize()
	maxOffset := intMax(0, len(pack.Files)-pageSize)

	switch {
	case key == "esc" || key == "backspace":
		m.screen = screenList
		m.detailOffset = 0
		m.versionEdit = versionEditNone

	case isManifestKey(key):
		m.screen = screenManifestLoading
		m.manifest = nil
		return m, buildManifestCmd(pack)

	case isPublishKey(key):
		m.resetProgress()
		m.screen = screenProgress

		progressCh := make(chan publish.ProgressUpdate, 128)

		return m, tea.Batch(
			publishCmd(m.cfg, m.root, pack, progressCh, m.versionEdit),
			listenProgressCmd(progressCh),
		)

	case key == "1":
		m.versionEdit = versionEditPatch

	case key == "2":
		m.versionEdit = versionEditMinor

	case key == "3":
		m.versionEdit = versionEditMajor

	case key == "0":
		m.versionEdit = versionEditNone

	case isGCKey(key):
		m.gcResult = nil
		m.returnScreen = screenLocalDetails
		m.screen = screenGCConfirm

	default:
		handlePagedNavigation(key, &m.detailOffset, maxOffset, pageSize)
	}

	return m, nil
}

func (m *Model) updateServerDetails(key string) (tea.Model, tea.Cmd) {
	if len(m.serverPacks) == 0 {
		m.screen = screenList
		return m, nil
	}

	switch {
	case key == "esc" || key == "backspace":
		m.screen = screenList

	case isDeleteKey(key):
		m.deleteResult = nil
		m.returnScreen = screenServerDetails
		m.screen = screenDeleteConfirm

	case isHistoryKey(key):
		pack := m.serverPacks[m.serverCursor]
		m.serverHistory = nil
		m.screen = screenServerHistoryLoading
		return m, loadServerHistoryCmd(m.cfg, pack.PackID)

	case isGCKey(key):
		m.gcResult = nil
		m.returnScreen = screenServerDetails
		m.screen = screenGCConfirm
	}

	return m, nil
}

func (m *Model) updateServerHistoryLoading(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		m.screen = screenServerDetails
	}
	return m, nil
}

func (m *Model) updateServerHistory(key string) (tea.Model, tea.Cmd) {
	pageSize := m.historyPageSize()
	maxOffset := intMax(0, len(m.serverHistory)-pageSize)

	switch {
	case key == "esc" || key == "backspace":
		m.screen = screenServerDetails
		m.historyOffset = 0

	default:
		handlePagedNavigation(key, &m.historyOffset, maxOffset, pageSize)
	}

	return m, nil
}

func (m *Model) updateManifestLoading(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		m.screen = screenLocalDetails
	}
	return m, nil
}

func (m *Model) updateManifest(key string) (tea.Model, tea.Cmd) {
	if m.manifest == nil {
		m.screen = screenLocalDetails
		return m, nil
	}

	pageSize := m.manifestPageSize()
	maxOffset := intMax(0, len(m.manifest.Files)-pageSize)

	switch {
	case key == "esc" || key == "backspace":
		m.screen = screenLocalDetails
		m.manifestOffset = 0

	default:
		handlePagedNavigation(key, &m.manifestOffset, maxOffset, pageSize)
	}

	return m, nil
}

func (m *Model) updateProgress(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		if m.progressDone {
			m.screen = screenLocalDetails
		}
	case "enter":
		if m.progressDone {
			m.screen = screenPublishResult
		}
	}
	return m, nil
}

func (m *Model) updatePublishResult(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace", "enter":
		m.screen = screenLocalDetails
	}
	return m, nil
}

func (m *Model) updateDeleteConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		m.screen = m.returnScreen
	case "enter":
		if len(m.serverPacks) == 0 {
			m.screen = screenList
			return m, nil
		}

		m.screen = screenDeleteLoading
		m.deleteResult = nil
		serverPack := m.serverPacks[m.serverCursor]
		return m, deleteCmd(m.cfg, serverPack)
	}

	return m, nil
}

func (m *Model) updateDeleteLoading(_ string) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *Model) updateDeleteResult(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace", "enter":
		m.screen = screenList
	}
	return m, nil
}

func (m *Model) updateGCConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		m.screen = m.returnScreen
	case "enter":
		m.screen = screenGCLoading
		m.gcResult = nil
		return m, gcCmd(m.cfg)
	}
	return m, nil
}

func (m *Model) updateGCLoading(_ string) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *Model) updateGCResult(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace", "enter":
		m.screen = m.returnScreen
	}
	return m, nil
}

func (m *Model) View() string {
	switch m.screen {
	case screenList:
		return appStyle.Render(m.viewList())
	case screenLocalDetails:
		return appStyle.Render(m.viewLocalDetails())
	case screenServerDetails:
		return appStyle.Render(m.viewServerDetails())
	case screenServerHistoryLoading:
		return appStyle.Render(m.viewLoadingScreen("Loading server history", "Manifests are being read for the selected pack."))
	case screenServerHistory:
		return appStyle.Render(m.viewServerHistory())
	case screenManifestLoading:
		return appStyle.Render(m.viewLoadingScreen("Building manifest", "SHA256 is being calculated for all files."))
	case screenManifest:
		return appStyle.Render(m.viewManifest())
	case screenProgress:
		return appStyle.Render(m.viewProgress())
	case screenPublishResult:
		return appStyle.Render(m.viewPublishResult())
	case screenDeleteConfirm:
		return appStyle.Render(m.viewDeleteConfirm())
	case screenDeleteLoading:
		return appStyle.Render(m.viewLoadingScreen("Deleting published metadata", "latest.json and manifests are being removed for the selected server pack."))
	case screenDeleteResult:
		return appStyle.Render(m.viewDeleteResult())
	case screenGCConfirm:
		return appStyle.Render(m.viewGCConfirm())
	case screenGCLoading:
		return appStyle.Render(m.viewLoadingScreen("GC unused objects", "Manifests are being scanned and orphan objects are being deleted."))
	case screenGCResult:
		return appStyle.Render(m.viewGCResult())
	default:
		return appStyle.Render(m.viewList())
	}
}

func (m *Model) viewList() string {
	var b strings.Builder

	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n")
	b.WriteString(kv("Scan Root", m.root))
	b.WriteString("\n")
	b.WriteString(kv("S3 Usage", m.viewUsageLine()))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(renderErrorBlock(m.err.Error()))
		b.WriteString("\n\n")
	}

	b.WriteString(m.renderServerPane())
	b.WriteString("\n\n")
	b.WriteString(m.renderLocalPane())
	b.WriteString("\n\n")
	b.WriteString(renderHint("[Tab] Pane  [Enter] Open  [R] Rescan  [G] GC  [Q] Quit"))

	return b.String()
}

func (m *Model) viewUsageLine() string {
	if m.s3UsageErr != nil {
		return "error: " + m.s3UsageErr.Error()
	}
	if !m.s3UsageLoaded {
		return "loading..."
	}
	return fmt.Sprintf(
		"%s / %s (%.1f%%)",
		formatBytes(m.s3UsedBytes),
		formatBytes(m.s3CapacityBytes),
		calcPercent(m.s3UsedBytes, m.s3CapacityBytes),
	)
}

func (m *Model) renderServerPane() string {
	rows := make([]string, 0, len(m.serverPacks))
	start, end := m.serverVisibleRange()

	for i := start; i < end; i++ {
		rows = append(rows, m.renderServerRow(i, m.serverPacks[i]))
	}

	return m.renderTableSection(
		"☁ Server Packs",
		m.focusedPane == paneServer,
		m.serverTableHeader(),
		rows,
		"No server packs published yet.",
	)
}

func (m *Model) renderLocalPane() string {
	rows := make([]string, 0, len(m.packs))
	start, end := m.localVisibleRange()

	for i := start; i < end; i++ {
		rows = append(rows, m.renderLocalRow(i, m.packs[i]))
	}

	return m.renderTableSection(
		"● Local Instances",
		m.focusedPane == paneLocal,
		m.localTableHeader(),
		rows,
		"No local instances found.",
	)
}

func (m *Model) renderTableSection(title string, active bool, header string, rows []string, emptyText string) string {
	var b strings.Builder

	b.WriteString(m.sectionTitle(title, active))
	b.WriteString("\n\n")
	b.WriteString(renderTableHeader(header))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(mutedStyle.Render(emptyText))
		return b.String()
	}

	for _, row := range rows {
		b.WriteString(row)
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) sectionTitle(title string, active bool) string {
	if active {
		return renderSectionTitle(title + "  " + chipInfo("active"))
	}
	return mutedStyle.Render(title)
}

func (m *Model) serverTableHeader() string {
	nameW := m.serverNameWidth()
	return "  " +
		padRight("Name", nameW) + "  " +
		padRight("Version", 10) + "  " +
		padRight("Size", 10) + "  " +
		"Published"
}

func (m *Model) localTableHeader() string {
	nameW := m.localNameWidth()
	return "  " +
		padRight("Name", nameW) + "  " +
		padRight("Version", 10) + "  " +
		padRight("Size", 10) + "  " +
		"Modified"
}

func renderRowPrefix(selected bool, name string, nameWidth int) string {
	cursor := "  "
	style := rowTextStyle

	if selected {
		cursor = cursorStyle.Render("› ")
		style = selectedTextStyle
	}

	nameText := padRight(truncate(name, nameWidth), nameWidth)
	return cursor + style.Render(nameText)
}

func (m *Model) renderServerRow(index int, pack serverpacks.Pack) string {
	version := nonEmptyText(pack.Version, "-")
	return renderRowPrefix(
		m.focusedPane == paneServer && index == m.serverCursor,
		pack.PackName,
		m.serverNameWidth(),
	) + "  " +
		padRight(version, 10) + "  " +
		padRight(formatBytes(pack.SizeBytes), 10) + "  " +
		formatTime(pack.PublishedAt)
}

func (m *Model) renderLocalRow(index int, pack packs.Pack) string {
	version := nonEmptyText(pack.Version, "-")
	return renderRowPrefix(
		m.focusedPane == paneLocal && index == m.localCursor,
		pack.Name,
		m.localNameWidth(),
	) + "  " +
		padRight(version, 10) + "  " +
		padRight(formatBytes(pack.SizeBytes), 10) + "  " +
		formatTime(pack.LastModified)
}

func (m *Model) viewLocalDetails() string {
	pack, ok := m.selectedLocalPack()
	if !ok {
		return m.viewList()
	}

	pageSize := m.filePageSize()
	start, end := visibleRange(len(pack.Files), m.detailOffset, pageSize)

	packID := strings.TrimSpace(pack.PackID)
	if packID == "" {
		packID = "(will be created on first publish)"
	}

	currentVersion := nonEmptyText(pack.Version, "0.1.0")
	versionLine := currentVersion
	if m.versionEdit != versionEditNone {
		nextVersion := previewVersion(currentVersion, m.versionEdit.PublishMode(), 1)
		versionLine = currentVersion + " → " + nextVersion
	}

	var b strings.Builder

	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle("◇ Local Instance"))
	b.WriteString("\n\n")
	b.WriteString(kv("Name", pack.Name))
	b.WriteString("\n")
	b.WriteString(kv("Folder", pack.FolderName))
	b.WriteString("\n")
	b.WriteString(kv("Pack ID", packID))
	b.WriteString("\n")
	b.WriteString(kv("Version", versionLine))
	b.WriteString("\n")
	b.WriteString(kv("Edit Mode", renderVersionEditModeLine(m.versionEdit)))
	b.WriteString("\n")
	b.WriteString(kv("Path", pack.Path))
	b.WriteString("\n")
	b.WriteString(kv("Size", formatBytes(pack.SizeBytes)))
	b.WriteString("\n")
	b.WriteString(kv("Files", fmt.Sprintf("%d", pack.FileCount)))
	b.WriteString("\n")
	b.WriteString(kv("Last Modified", formatTime(pack.LastModified)))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle("Files"))
	b.WriteString("\n\n")

	if len(pack.Files) == 0 {
		b.WriteString(mutedStyle.Render("(empty)"))
	} else {
		for _, file := range pack.Files[start:end] {
			b.WriteString(padRight(formatBytes(file.SizeBytes), 10))
			b.WriteString("  ")
			b.WriteString(file.RelativePath)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("Showing %d-%d of %d", start+1, end, len(pack.Files))))
	}

	b.WriteString("\n\n")
	b.WriteString(renderHint("[M] Manifest  [P] Publish  [1/2/3] Select part  [0] Clear  [G] GC"))
	b.WriteString("\n")
	b.WriteString(renderHint("[↑/↓] Scroll  [PgUp/PgDn] Page  [Esc] Back  [R] Rescan  [Q] Quit"))

	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewServerDetails() string {
	pack, ok := m.selectedServerPack()
	if !ok {
		return m.viewList()
	}

	var b strings.Builder
	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle("☁ Server Pack"))
	b.WriteString("\n\n")
	b.WriteString(kv("Name", pack.PackName))
	b.WriteString("\n")
	b.WriteString(kv("Version", nonEmptyText(pack.Version, "-")))
	b.WriteString("\n")
	b.WriteString(kv("Pack ID", pack.PackID))
	b.WriteString("\n")
	b.WriteString(kv("Pack Slug", pack.PackSlug))
	b.WriteString("\n")
	b.WriteString(kv("Size", formatBytes(pack.SizeBytes)))
	b.WriteString("\n")
	b.WriteString(kv("Files", fmt.Sprintf("%d", pack.FileCount)))
	b.WriteString("\n")
	b.WriteString(kv("Published At", formatTime(pack.PublishedAt)))
	b.WriteString("\n")
	b.WriteString(kv("Manifest Key", pack.ManifestKey))
	b.WriteString("\n\n")
	b.WriteString(renderHint("[D] Delete metadata  [V] History  [G] GC  [Esc] Back  [R] Rescan  [Q] Quit"))
	return b.String()
}

func (m *Model) viewServerHistory() string {
	var b strings.Builder

	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle("Server Pack History"))
	b.WriteString("\n\n")

	if len(m.serverHistory) == 0 {
		b.WriteString(mutedStyle.Render("(empty)"))
		b.WriteString("\n\n")
		b.WriteString(renderHint("[Esc] Back  [Q] Quit"))
		return b.String()
	}

	pageSize := m.historyPageSize()
	start, end := visibleRange(len(m.serverHistory), m.historyOffset, pageSize)

	for _, entry := range m.serverHistory[start:end] {
		b.WriteString(padRight(nonEmptyText(entry.Version, "-"), 10))
		b.WriteString("  ")
		b.WriteString(padRight(formatBytes(entry.SizeBytes), 10))
		b.WriteString("  ")
		b.WriteString(entry.GeneratedAt.Format("2006-01-02 15:04:05"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("  Manifest: " + entry.ManifestKey))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  Files:    %d", entry.FileCount)))
		b.WriteString("\n\n")
	}

	b.WriteString(mutedStyle.Render(fmt.Sprintf("Showing %d-%d of %d", start+1, end, len(m.serverHistory))))
	b.WriteString("\n\n")
	b.WriteString(renderHint("[↑/↓] Scroll  [PgUp/PgDn] Page  [Esc] Back  [Q] Quit"))

	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewManifest() string {
	if m.manifest == nil {
		return m.viewLocalDetails()
	}

	pageSize := m.manifestPageSize()
	start, end := visibleRange(len(m.manifest.Files), m.manifestOffset, pageSize)

	var b strings.Builder

	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle("Manifest"))
	b.WriteString("\n\n")
	b.WriteString(kv("Pack", m.manifest.PackName))
	b.WriteString("\n")
	b.WriteString(kv("Version", m.manifest.Version))
	b.WriteString("\n")
	b.WriteString(kv("Generated At", m.manifest.GeneratedAt.Format("2006-01-02 15:04:05")))
	b.WriteString("\n")
	b.WriteString(kv("Files", fmt.Sprintf("%d", m.manifest.FileCount)))
	b.WriteString("\n")
	b.WriteString(kv("Size", formatBytes(m.manifest.SizeBytes)))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle("Entries"))
	b.WriteString("\n\n")

	if len(m.manifest.Files) == 0 {
		b.WriteString(mutedStyle.Render("(empty)"))
	} else {
		for _, file := range m.manifest.Files[start:end] {
			b.WriteString(padRight(formatBytes(file.SizeBytes), 10))
			b.WriteString("  ")
			b.WriteString(padRight(shortHash(file.SHA256), 14))
			b.WriteString("  ")
			b.WriteString(file.Path)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("Showing %d-%d of %d", start+1, end, len(m.manifest.Files))))
	}

	b.WriteString("\n\n")
	b.WriteString(renderHint("[↑/↓] Scroll  [PgUp/PgDn] Page  [Esc] Back  [R] Rescan  [Q] Quit"))

	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewProgress() string {
	var b strings.Builder

	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle("↑ Publishing to S3"))
	b.WriteString("\n\n")
	b.WriteString(kv("Stage", nonEmptyText(m.progressStage, "preparing")))
	b.WriteString("\n")
	b.WriteString(kv("File", nonEmptyText(m.progressCurrentFile, "-")))
	b.WriteString("\n")
	b.WriteString(kv("Files", fmt.Sprintf("%d / %d", m.progressFilesDone, m.progressFilesTotal)))
	b.WriteString("\n")
	if m.progressBytesTotal > 0 {
		b.WriteString(kv("Bytes", fmt.Sprintf("%s / %s", formatBytes(m.progressBytesDone), formatBytes(m.progressBytesTotal))))
	} else {
		b.WriteString(kv("Bytes", fmt.Sprintf("%s / -", formatBytes(m.progressBytesDone))))
	}
	b.WriteString("\n")
	b.WriteString(kv("Done", formatPercent(m.progressPercent)))
	b.WriteString("\n\n")
	b.WriteString(renderProgressBar(m.progressPercent, 36))

	if m.progressDone && m.progressErr != nil {
		b.WriteString("\n\n")
		b.WriteString(renderErrorBlock(m.progressErr.Error()))
	}

	if m.progressDone && m.publishResult != nil && m.progressErr == nil {
		b.WriteString("\n\n")
		b.WriteString(kv("Uploaded", fmt.Sprintf("%d", m.publishResult.UploadedObjects)))
		b.WriteString("\n")
		b.WriteString(kv("Skipped", fmt.Sprintf("%d", m.publishResult.SkippedObjects)))
	}

	b.WriteString("\n\n")
	if m.progressDone {
		if m.progressErr != nil {
			b.WriteString(renderHint("[Esc] Back  [Q] Quit"))
		} else {
			b.WriteString(renderHint("[Enter] Result  [Esc] Back  [Q] Quit"))
		}
	} else {
		b.WriteString(renderHint("[Q] Quit"))
	}

	return b.String()
}

func (m *Model) viewPublishResult() string {
	lines := func() [][2]string {
		if m.publishResult == nil {
			return nil
		}
		r := m.publishResult
		return [][2]string{
			{"Pack", r.PackName},
			{"Version", r.Version},
			{"Pack ID", r.PackID},
			{"Published At", r.PublishedAt.Format("2006-01-02 15:04:05")},
			{"Uploaded Objects", fmt.Sprintf("%d", r.UploadedObjects)},
			{"Skipped Objects", fmt.Sprintf("%d", r.SkippedObjects)},
			{"Manifest Key", r.ManifestKey},
			{"Latest Key", r.LatestKey},
			{"Manifest Size", fmt.Sprintf("%d bytes", r.ManifestFileSize)},
		}
	}()

	return m.renderResultScreen(
		"✓ Publish Finished",
		m.err,
		lines,
		"No publish result.",
		"[Enter] Back  [Esc] Back  [Q] Quit",
	)
}

func (m *Model) viewDeleteConfirm() string {
	pack, ok := m.selectedServerPack()
	if !ok {
		return m.viewList()
	}

	var body strings.Builder
	body.WriteString(kv("Pack", pack.PackName))
	body.WriteString("\n")
	body.WriteString(kv("Version", nonEmptyText(pack.Version, "-")))
	body.WriteString("\n")
	body.WriteString(kv("Pack ID", pack.PackID))
	body.WriteString("\n\n")
	body.WriteString("This will remove only:\n")
	body.WriteString("- packs/<pack_id>/latest.json\n")
	body.WriteString("- packs/<pack_id>/manifests/*\n")
	body.WriteString("\nIt will NOT remove:\n")
	body.WriteString("- local instance folder\n")
	body.WriteString("- shared objects/* in S3\n")
	body.WriteString("\nS3 size will decrease only after GC.")

	return m.renderConfirmScreen(
		"Delete published metadata",
		body.String(),
		"[Enter] Confirm delete  [Esc] Cancel  [Q] Quit",
	)
}

func (m *Model) viewDeleteResult() string {
	lines := func() [][2]string {
		if m.deleteResult == nil {
			return nil
		}
		r := m.deleteResult
		return [][2]string{
			{"Pack", r.PackName},
			{"Pack ID", r.PackID},
			{"Pack Slug", r.PackSlug},
			{"Prefix", r.Prefix},
			{"Deleted Keys", fmt.Sprintf("%d", r.DeletedKeys)},
			{"Deleted At", r.DeletedAt.Format("2006-01-02 15:04:05")},
		}
	}()

	return m.renderResultScreen(
		"✓ Published Metadata Deleted",
		m.err,
		lines,
		"No delete result.",
		"[Enter] Back  [Esc] Back  [Q] Quit",
	)
}

func (m *Model) viewGCConfirm() string {
	body := "This will scan all manifests and delete unreferenced objects/* in S3."
	return m.renderConfirmScreen(
		"GC unused objects",
		body,
		"[Enter] Run GC  [Esc] Cancel  [Q] Quit",
	)
}

func (m *Model) viewGCResult() string {
	lines := func() [][2]string {
		if m.gcResult == nil {
			return nil
		}
		r := m.gcResult
		return [][2]string{
			{"Manifest scanned", fmt.Sprintf("%d", r.ManifestKeysScanned)},
			{"Referenced objects", fmt.Sprintf("%d", r.ReferencedObjects)},
			{"Objects scanned", fmt.Sprintf("%d", r.ObjectsScanned)},
			{"Deleted objects", fmt.Sprintf("%d", r.DeletedObjects)},
			{"Deleted bytes", formatBytes(r.DeletedBytes)},
			{"Completed at", r.CompletedAt.Format("2006-01-02 15:04:05")},
		}
	}()

	return m.renderResultScreen(
		"✓ GC Finished",
		m.err,
		lines,
		"No GC result.",
		"[Enter] Back  [Esc] Back  [Q] Quit",
	)
}

func (m *Model) viewLoadingScreen(title, text string) string {
	var b strings.Builder
	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle(title))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(text))
	b.WriteString("\n\n")
	b.WriteString(renderHint("[Q] Quit"))
	return b.String()
}

func (m *Model) renderConfirmScreen(title, body, hint string) string {
	var b strings.Builder
	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")
	b.WriteString(renderSectionTitle(title))
	b.WriteString("\n\n")
	b.WriteString(body)
	b.WriteString("\n\n")
	b.WriteString(renderHint(hint))
	return b.String()
}

func (m *Model) renderResultScreen(title string, opErr error, lines [][2]string, emptyText, hint string) string {
	var b strings.Builder
	b.WriteString(appHeader("v1.0.0"))
	b.WriteString("\n")
	b.WriteString(renderDivider(m.width))
	b.WriteString("\n\n")

	switch {
	case opErr != nil:
		b.WriteString(renderErrorBlock(opErr.Error()))
	case lines == nil:
		b.WriteString(renderInfoBlock("No result", emptyText))
	default:
		b.WriteString(renderSectionTitle(title))
		b.WriteString("\n\n")
		b.WriteString(linesToBody(lines))
	}

	b.WriteString("\n\n")
	b.WriteString(renderHint(hint))
	return b.String()
}

func linesToBody(lines [][2]string) string {
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(kv(line[0], line[1]))
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *Model) selectedLocalPack() (packs.Pack, bool) {
	if len(m.packs) == 0 || m.localCursor < 0 || m.localCursor >= len(m.packs) {
		return packs.Pack{}, false
	}
	return m.packs[m.localCursor], true
}

func (m *Model) selectedServerPack() (serverpacks.Pack, bool) {
	if len(m.serverPacks) == 0 || m.serverCursor < 0 || m.serverCursor >= len(m.serverPacks) {
		return serverpacks.Pack{}, false
	}
	return m.serverPacks[m.serverCursor], true
}

func (m *Model) switchPane() {
	if m.focusedPane == paneLocal {
		if len(m.serverPacks) > 0 || len(m.packs) == 0 {
			m.focusedPane = paneServer
		}
		return
	}

	if len(m.packs) > 0 || len(m.serverPacks) == 0 {
		m.focusedPane = paneLocal
	}
}

func (m *Model) localVisibleRange() (int, int) {
	rows := m.localListPageSize()
	start := intMin(m.localListStart(), intMax(0, len(m.packs)-rows))
	end := intMin(len(m.packs), start+rows)
	return start, end
}

func (m *Model) serverVisibleRange() (int, int) {
	rows := m.serverListPageSize()
	start := intMin(m.serverListStart(), intMax(0, len(m.serverPacks)-rows))
	end := intMin(len(m.serverPacks), start+rows)
	return start, end
}

func (m *Model) localNameWidth() int {
	w := 24
	if m.width >= 100 {
		w = 30
	}
	if m.width >= 120 {
		w = 36
	}
	return w
}

func (m *Model) serverNameWidth() int {
	w := 28
	if m.width >= 100 {
		w = 34
	}
	if m.width >= 120 {
		w = 40
	}
	return w
}

func (m *Model) handleProgressMessage(msg progressMsg) (tea.Model, tea.Cmd) {
	m.progressStage = msg.update.Stage
	m.progressCurrentFile = msg.update.CurrentFile
	m.progressFilesDone = msg.update.FilesDone
	m.progressFilesTotal = msg.update.FilesTotal
	m.progressBytesDone = msg.update.BytesDone
	m.progressBytesTotal = msg.update.BytesTotal
	m.progressDone = msg.update.Done
	m.progressErr = msg.update.Err

	if m.progressBytesTotal > 0 {
		m.progressPercent = (float64(m.progressBytesDone) / float64(m.progressBytesTotal)) * 100
		if m.progressPercent > 100 {
			m.progressPercent = 100
		}
	} else if m.progressFilesTotal > 0 {
		m.progressPercent = (float64(m.progressFilesDone) / float64(m.progressFilesTotal)) * 100
		if m.progressPercent > 100 {
			m.progressPercent = 100
		}
	} else {
		m.progressPercent = 0
	}

	if msg.update.Done {
		if msg.update.Err != nil {
			m.err = msg.update.Err
		}
		return m, nil
	}

	return m, listenProgressCmd(msg.ch)
}

func scanCmd(root string) tea.Cmd {
	return func() tea.Msg {
		packsList, err := packs.ScanRoot(root)
		return packsLoadedMsg{packs: packsList, err: err}
	}
}

func loadUsageCmd(cfg appconfig.Config) tea.Cmd {
	return withStoreCmd(
		cfg,
		func(ctx context.Context, store *storage.Client) (int64, error) {
			return store.ListUsedBytes(ctx)
		},
		func(result int64, err error) tea.Msg {
			return usageLoadedMsg{usedBytes: result, err: err}
		},
	)
}

func loadServerPacksCmd(cfg appconfig.Config) tea.Cmd {
	return withStoreCmd(
		cfg,
		func(ctx context.Context, store *storage.Client) ([]serverpacks.Pack, error) {
			return serverpacks.List(ctx, store)
		},
		func(result []serverpacks.Pack, err error) tea.Msg {
			return serverPacksLoadedMsg{packs: result, err: err}
		},
	)
}

func loadServerHistoryCmd(cfg appconfig.Config, packID string) tea.Cmd {
	return withStoreCmd(
		cfg,
		func(ctx context.Context, store *storage.Client) ([]serverpacks.HistoryEntry, error) {
			return serverpacks.ListHistory(ctx, store, packID)
		},
		func(result []serverpacks.HistoryEntry, err error) tea.Msg {
			return serverHistoryLoadedMsg{history: result, err: err}
		},
	)
}

func buildManifestCmd(pack packs.Pack) tea.Cmd {
	return func() tea.Msg {
		mf, err := manifest.Build(pack)
		return manifestBuiltMsg{manifest: mf, err: err}
	}
}

func publishCmd(
	cfg appconfig.Config,
	root string,
	pack packs.Pack,
	progressCh chan publish.ProgressUpdate,
	versionEdit versionEditMode,
) tea.Cmd {
	return func() tea.Msg {
		defer close(progressCh)

		ctx := context.Background()

		store, err := storage.New(ctx, cfg)
		if err != nil {
			return publishDoneMsg{err: err}
		}

		if err := store.HeadBucket(ctx); err != nil {
			return publishDoneMsg{err: err}
		}

		freshPack, err := rescanPackByPath(root, pack.Path, pack.FolderName)
		if err != nil {
			return publishDoneMsg{err: err}
		}

		effectivePack := freshPack
		commitVersionAfterPublish := false

		if versionEdit != versionEditNone {
			meta, err := packmeta.Ensure(freshPack.Path, freshPack.FolderName)
			if err != nil {
				return publishDoneMsg{err: err}
			}

			currentVersion := packmeta.VisibleVersion(meta)
			nextVersion, err := packmeta.PreviewVersion(currentVersion, versionEdit.PublishMode(), 1)
			if err != nil {
				return publishDoneMsg{err: err}
			}

			effectivePack.Name = packmeta.VisibleName(meta, freshPack.FolderName)
			effectivePack.PackID = meta.PackID
			effectivePack.Version = nextVersion
			commitVersionAfterPublish = true
		}

		result, err := publish.PackWithProgress(ctx, store, effectivePack, progressCh)
		if err != nil {
			return publishDoneMsg{result: result, err: err}
		}

		if _, err := packcleanup.AfterPublish(ctx, store, result.PackID, result.ManifestKey); err != nil {
			return publishDoneMsg{result: result, err: err}
		}

		if commitVersionAfterPublish {
			meta, err := packmeta.Ensure(freshPack.Path, freshPack.FolderName)
			if err != nil {
				return publishDoneMsg{result: result, err: err}
			}

			meta.Version = effectivePack.Version
			if err := packmeta.Save(freshPack.Path, meta); err != nil {
				return publishDoneMsg{result: result, err: err}
			}
		}

		return publishDoneMsg{result: result, err: nil}
	}
}

func deleteCmd(cfg appconfig.Config, pack serverpacks.Pack) tea.Cmd {
	return withStoreCmd(
		cfg,
		func(ctx context.Context, store *storage.Client) (unpublish.Result, error) {
			if err := store.HeadBucket(ctx); err != nil {
				return unpublish.Result{}, err
			}
			return unpublish.PackByID(ctx, store, pack.PackID, pack.PackName, pack.PackSlug)
		},
		func(result unpublish.Result, err error) tea.Msg {
			return deleteDoneMsg{result: result, err: err}
		},
	)
}

func gcCmd(cfg appconfig.Config) tea.Cmd {
	return withStoreCmd(
		cfg,
		func(ctx context.Context, store *storage.Client) (gcsvc.Result, error) {
			if err := store.HeadBucket(ctx); err != nil {
				return gcsvc.Result{}, err
			}
			return gcsvc.Sweep(ctx, store)
		},
		func(result gcsvc.Result, err error) tea.Msg {
			return gcDoneMsg{result: result, err: err}
		},
	)
}

func rebuildPublicIndexCmd(cfg appconfig.Config) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		store, err := storage.New(ctx, cfg)
		if err != nil {
			return nil
		}

		_ = publicindex.Publish(ctx, store)
		return nil
	}
}

func withStoreCmd[T any](
	cfg appconfig.Config,
	fn func(context.Context, *storage.Client) (T, error),
	wrap func(T, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		store, err := storage.New(ctx, cfg)
		if err != nil {
			var zero T
			return wrap(zero, err)
		}

		result, err := fn(ctx, store)
		return wrap(result, err)
	}
}

func listenProgressCmd(ch <-chan publish.ProgressUpdate) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-ch
		if !ok {
			return nil
		}
		return progressMsg{update: update, ch: ch}
	}
}

func (m *Model) resetProgress() {
	m.progressStage = ""
	m.progressCurrentFile = ""
	m.progressFilesDone = 0
	m.progressFilesTotal = 0
	m.progressBytesDone = 0
	m.progressBytesTotal = 0
	m.progressPercent = 0
	m.progressDone = false
	m.progressErr = nil
}

func (m *Model) filePageSize() int {
	if m.height <= 0 {
		return 20
	}
	return intMax(5, m.height-18)
}

func (m *Model) manifestPageSize() int {
	if m.height <= 0 {
		return 20
	}
	return intMax(5, m.height-16)
}

func (m *Model) historyPageSize() int {
	if m.height <= 0 {
		return 8
	}
	return intMax(4, m.height/2)
}

func (m *Model) localListPageSize() int {
	if m.height <= 0 {
		return 8
	}
	return intMax(4, m.height/3)
}

func (m *Model) serverListPageSize() int {
	if m.height <= 0 {
		return 8
	}
	return intMax(4, m.height/3)
}

func (m *Model) localListStart() int {
	rows := m.localListPageSize()
	if m.localCursor < rows {
		return 0
	}
	return m.localCursor - rows + 1
}

func (m *Model) serverListStart() int {
	rows := m.serverListPageSize()
	if m.serverCursor < rows {
		return 0
	}
	return m.serverCursor - rows + 1
}

func previewVersion(version, mode string, delta int) string {
	next, err := packmeta.PreviewVersion(version, mode, delta)
	if err != nil {
		return version
	}
	return next
}

func renderVersionEditModeLine(mode versionEditMode) string {
	patch := "[1] PATCH"
	minor := "[2] MINOR"
	major := "[3] MAJOR"

	switch mode {
	case versionEditPatch:
		patch = chipInfo("1 PATCH")
	case versionEditMinor:
		minor = chipInfo("2 MINOR")
	case versionEditMajor:
		major = chipInfo("3 MAJOR")
	}

	return patch + "  " + minor + "  " + major + "  [0] CLEAR"
}

func visibleRange(total, offset, pageSize int) (int, int) {
	start := intMin(offset, intMax(0, total-pageSize))
	end := intMin(total, start+pageSize)
	return start, end
}

func handlePagedNavigation(key string, offset *int, maxOffset, pageSize int) {
	switch {
	case isUpKey(key):
		if *offset > 0 {
			*offset--
		}
	case isDownKey(key):
		if *offset < maxOffset {
			*offset++
		}
	case isPageUpKey(key):
		*offset -= pageSize
		if *offset < 0 {
			*offset = 0
		}
	case isPageDownKey(key):
		*offset += pageSize
		if *offset > maxOffset {
			*offset = maxOffset
		}
	case key == "home":
		*offset = 0
	case key == "end":
		*offset = maxOffset
	}
}

func formatBytes(size int64) string {
	const unit = 1024

	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	suffixes := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(size) / float64(div)

	return fmt.Sprintf("%.2f %s", value, suffixes[exp])
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func calcPercent(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	p := (float64(used) / float64(total)) * 100
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)

	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func nonEmptyText(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func isQuitKey(s string) bool     { return s == "q" || s == "й" }
func isRescanKey(s string) bool   { return s == "r" || s == "к" }
func isManifestKey(s string) bool { return s == "m" || s == "ь" }
func isPublishKey(s string) bool  { return s == "p" || s == "з" }
func isDeleteKey(s string) bool   { return s == "d" || s == "в" }
func isGCKey(s string) bool       { return s == "g" || s == "п" }
func isHistoryKey(s string) bool  { return s == "v" || s == "м" }
func isUpKey(s string) bool       { return s == "up" || s == "k" || s == "л" }
func isDownKey(s string) bool     { return s == "down" || s == "j" || s == "о" }

// noinspection SpellCheckingInspection
func isPageUpKey(s string) bool { return s == "pgup" }

// noinspection SpellCheckingInspection
func isPageDownKey(s string) bool { return s == "pgdown" }

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func rescanPackByPath(root, targetPath, targetFolderName string) (packs.Pack, error) {
	scanned, err := packs.ScanRoot(root)
	if err != nil {
		return packs.Pack{}, err
	}

	cleanTargetPath := filepath.Clean(targetPath)

	for _, p := range scanned {
		if filepath.Clean(p.Path) == cleanTargetPath {
			return p, nil
		}
	}

	for _, p := range scanned {
		if strings.TrimSpace(p.FolderName) == strings.TrimSpace(targetFolderName) {
			return p, nil
		}
	}

	return packs.Pack{}, fmt.Errorf("pack not found after rescan: %s", targetFolderName)
}
