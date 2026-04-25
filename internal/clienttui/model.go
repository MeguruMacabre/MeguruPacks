package clienttui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/appconfig"
	"github.com/MeguruMacabre/MeguruPacks/internal/autoupdate"
	"github.com/MeguruMacabre/MeguruPacks/internal/clientstate"
	"github.com/MeguruMacabre/MeguruPacks/internal/install"
	"github.com/MeguruMacabre/MeguruPacks/internal/reinstall"
	"github.com/MeguruMacabre/MeguruPacks/internal/removelocal"
	"github.com/MeguruMacabre/MeguruPacks/internal/serverpacks"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
	updatepkg "github.com/MeguruMacabre/MeguruPacks/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int
type listSection int

const (
	screenStartupSync screen = iota
	screenList
	screenDetails
	screenInstallProgress
	screenInstallResult
	screenUpdateProgress
	screenUpdateResult
	screenReinstallProgress
	screenReinstallResult
	screenDeleteLocalConfirm
	screenDeleteLocalResult
)

const (
	sectionInstalled listSection = iota
	sectionAvailable
)

type serverPacksLoadedMsg struct {
	packs []serverpacks.Pack
	err   error
}

type installDoneMsg struct {
	result install.Result
	err    error
}

type installProgressMsg struct {
	update install.ProgressUpdate
	ch     <-chan install.ProgressUpdate
}

type updateDoneMsg struct {
	result updatepkg.Result
	err    error
}

type updateProgressMsg struct {
	update updatepkg.ProgressUpdate
	ch     <-chan updatepkg.ProgressUpdate
}

type reinstallDoneMsg struct {
	result reinstall.Result
	err    error
}

type reinstallProgressMsg struct {
	update reinstall.ProgressUpdate
	ch     <-chan reinstall.ProgressUpdate
}

type deleteLocalDoneMsg struct {
	result removelocal.Result
	err    error
}

type startupSyncMsg struct {
	update autoupdate.StatusUpdate
	ch     <-chan autoupdate.StatusUpdate
}

type installedStatus struct {
	Installed   bool
	TargetDir   string
	LocalState  clientstate.State
	VersionText string
}

type packRow struct {
	Pack            serverpacks.Pack
	Status          installedStatus
	UpdateAvailable bool
}

type Model struct {
	cfg         appconfig.Config
	installRoot string

	screen screen

	packs []serverpacks.Pack

	activeSection   listSection
	installedCursor int
	availableCursor int

	width  int
	height int

	err error

	installResult   *install.Result
	updateResult    *updatepkg.Result
	reinstallResult *reinstall.Result
	deleteResult    *removelocal.Result

	progressStage       string
	progressCurrentFile string
	progressFilesDone   int
	progressFilesTotal  int
	progressBytesDone   int64
	progressBytesTotal  int64
	progressPercent     float64
	progressDone        bool
	progressErr         error

	startupStage   string
	startupPack    string
	startupCurrent int
	startupTotal   int
	startupDone    bool
	startupResult  *autoupdate.Result
}

func New(cfg appconfig.Config, installRoot string) *Model {
	return &Model{
		cfg:             cfg,
		installRoot:     installRoot,
		screen:          screenStartupSync,
		packs:           []serverpacks.Pack{},
		activeSection:   sectionInstalled,
		installedCursor: 0,
		availableCursor: 0,
	}
}

func (m *Model) Init() tea.Cmd {
	statusCh := make(chan autoupdate.StatusUpdate, 64)
	return tea.Batch(
		startupSyncCmd(m.cfg, m.installRoot, statusCh),
		listenStartupSyncCmd(statusCh),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case startupSyncMsg:
		m.startupStage = msg.update.Stage
		m.startupPack = msg.update.PackName
		m.startupCurrent = msg.update.Current
		m.startupTotal = msg.update.Total
		if msg.update.Result != nil {
			m.startupResult = msg.update.Result
		}
		m.startupDone = msg.update.Done
		if strings.TrimSpace(msg.update.ErrText) != "" {
			m.err = fmt.Errorf("%s", msg.update.ErrText)
		}

		if msg.update.Done {
			return m, loadServerPacksCmd(m.cfg)
		}
		return m, listenStartupSyncCmd(msg.ch)

	case serverPacksLoadedMsg:
		m.packs = msg.packs
		if msg.err != nil {
			m.err = msg.err
		}
		m.clampCursors()
		if m.screen == screenStartupSync {
			m.screen = screenList
		}
		return m, nil

	case installProgressMsg:
		return m.handleProgressMessage(
			listenInstallProgressCmd(msg.ch),
			msg.update.Stage,
			msg.update.CurrentFile,
			msg.update.FilesDone,
			msg.update.FilesTotal,
			msg.update.BytesDone,
			msg.update.BytesTotal,
			msg.update.Done,
			msg.update.Err,
		)

	case installDoneMsg:
		return m.finishOperation(
			screenInstallProgress,
			msg.err,
			func(err error) {
				m.installResult = nil
				m.progressErr = err
			},
			func() {
				m.installResult = &msg.result
			},
		)

	case updateProgressMsg:
		return m.handleProgressMessage(
			listenUpdateProgressCmd(msg.ch),
			msg.update.Stage,
			msg.update.CurrentFile,
			msg.update.FilesDone,
			msg.update.FilesTotal,
			msg.update.BytesDone,
			msg.update.BytesTotal,
			msg.update.Done,
			msg.update.Err,
		)

	case updateDoneMsg:
		return m.finishOperation(
			screenUpdateProgress,
			msg.err,
			func(err error) {
				m.updateResult = nil
				m.progressErr = err
			},
			func() {
				m.updateResult = &msg.result
			},
		)

	case reinstallProgressMsg:
		return m.handleProgressMessage(
			listenReinstallProgressCmd(msg.ch),
			msg.update.Stage,
			msg.update.CurrentFile,
			msg.update.FilesDone,
			msg.update.FilesTotal,
			msg.update.BytesDone,
			msg.update.BytesTotal,
			msg.update.Done,
			msg.update.Err,
		)

	case reinstallDoneMsg:
		return m.finishOperation(
			screenReinstallProgress,
			msg.err,
			func(err error) {
				m.reinstallResult = nil
				m.progressErr = err
			},
			func() {
				m.reinstallResult = &msg.result
			},
		)

	case deleteLocalDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.deleteResult = nil
		} else {
			m.deleteResult = &msg.result
		}
		m.screen = screenDeleteLocalResult
		return m, tea.Batch(loadServerPacksCmd(m.cfg))

	case tea.KeyMsg:
		key := msg.String()

		switch {
		case key == "ctrl+c" || isQuitKey(key):
			return m, tea.Quit
		case isRefreshKey(key):
			return m, loadServerPacksCmd(m.cfg)
		}

		switch m.screen {
		case screenStartupSync:
			return m.updateStartupSync(key)
		case screenList:
			return m.updateList(key)
		case screenDetails:
			return m.updateDetails(key)
		case screenInstallProgress:
			return m.updateInstallProgress(key)
		case screenInstallResult:
			return m.updateInstallResult(key)
		case screenUpdateProgress:
			return m.updateUpdateProgress(key)
		case screenUpdateResult:
			return m.updateUpdateResult(key)
		case screenReinstallProgress:
			return m.updateReinstallProgress(key)
		case screenReinstallResult:
			return m.updateReinstallResult(key)
		case screenDeleteLocalConfirm:
			return m.updateDeleteLocalConfirm(key)
		case screenDeleteLocalResult:
			return m.updateDeleteLocalResult(key)
		}
	}

	return m, nil
}

func (m *Model) updateStartupSync(_ string) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *Model) updateList(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab":
		m.switchSection()
		return m, nil
	case "shift+tab":
		m.switchSection()
		return m, nil
	case "enter":
		if _, ok := m.selectedRow(); ok {
			m.screen = screenDetails
		}
		return m, nil
	}

	switch {
	case isUpKey(key):
		m.moveCursor(-1)
	case isDownKey(key):
		m.moveCursor(1)
	}
	return m, nil
}

func (m *Model) updateDetails(key string) (tea.Model, tea.Cmd) {
	row, ok := m.selectedRow()
	if !ok {
		m.screen = screenList
		return m, nil
	}

	switch {
	case key == "esc" || key == "backspace":
		m.screen = screenList

	case isInstallKey(key):
		m.installResult = nil
		progressCh := make(chan install.ProgressUpdate, 128)
		return m.beginOperation(
			screenInstallProgress,
			installCmd(m.cfg, row.Pack, m.installRoot, progressCh),
			listenInstallProgressCmd(progressCh),
		)

	case isUpdateKey(key):
		if !row.Status.Installed {
			return m, nil
		}
		m.updateResult = nil
		progressCh := make(chan updatepkg.ProgressUpdate, 128)
		return m.beginOperation(
			screenUpdateProgress,
			updateCmd(m.cfg, row.Pack, m.installRoot, progressCh),
			listenUpdateProgressCmd(progressCh),
		)

	case isReinstallKey(key):
		if !row.Status.Installed {
			return m, nil
		}
		m.reinstallResult = nil
		progressCh := make(chan reinstall.ProgressUpdate, 128)
		return m.beginOperation(
			screenReinstallProgress,
			reinstallCmd(m.cfg, row.Pack, m.installRoot, progressCh),
			listenReinstallProgressCmd(progressCh),
		)

	case isDeleteKey(key):
		if !row.Status.Installed {
			return m, nil
		}
		m.deleteResult = nil
		m.screen = screenDeleteLocalConfirm
	}

	return m, nil
}

func (m *Model) updateInstallProgress(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		if m.progressDone {
			m.screen = screenDetails
		}
	case "enter":
		if m.progressDone {
			m.screen = screenInstallResult
		}
	}
	return m, nil
}

func (m *Model) updateInstallResult(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace", "enter":
		m.screen = screenDetails
	}
	return m, nil
}

func (m *Model) updateUpdateProgress(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		if m.progressDone {
			m.screen = screenDetails
		}
	case "enter":
		if m.progressDone {
			m.screen = screenUpdateResult
		}
	}
	return m, nil
}

func (m *Model) updateUpdateResult(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace", "enter":
		m.screen = screenDetails
	}
	return m, nil
}

func (m *Model) updateReinstallProgress(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace":
		if m.progressDone {
			m.screen = screenDetails
		}
	case "enter":
		if m.progressDone {
			m.screen = screenReinstallResult
		}
	}
	return m, nil
}

func (m *Model) updateReinstallResult(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace", "enter":
		m.screen = screenDetails
	}
	return m, nil
}

func (m *Model) updateDeleteLocalConfirm(key string) (tea.Model, tea.Cmd) {
	row, ok := m.selectedRow()
	if !ok {
		m.screen = screenList
		return m, nil
	}

	switch key {
	case "esc", "backspace":
		m.screen = screenDetails
	case "enter":
		return m, deleteLocalCmd(m.installRoot, row.Pack)
	}
	return m, nil
}

func (m *Model) updateDeleteLocalResult(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "backspace", "enter":
		m.screen = screenDetails
	}
	return m, nil
}

func (m *Model) View() string {
	switch m.screen {
	case screenStartupSync:
		return appStyle.Render(m.viewStartupSync())
	case screenDetails:
		return appStyle.Render(m.viewDetails())
	case screenInstallProgress:
		return appStyle.Render(m.viewInstallProgress())
	case screenInstallResult:
		return appStyle.Render(m.viewInstallResult())
	case screenUpdateProgress:
		return appStyle.Render(m.viewUpdateProgress())
	case screenUpdateResult:
		return appStyle.Render(m.viewUpdateResult())
	case screenReinstallProgress:
		return appStyle.Render(m.viewReinstallProgress())
	case screenReinstallResult:
		return appStyle.Render(m.viewReinstallResult())
	case screenDeleteLocalConfirm:
		return appStyle.Render(m.viewDeleteLocalConfirm())
	case screenDeleteLocalResult:
		return appStyle.Render(m.viewDeleteLocalResult())
	default:
		return appStyle.Render(m.viewList())
	}
}

func (m *Model) viewStartupSync() string {
	var out strings.Builder

	out.WriteString(appHeader("v1.0.0"))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n\n")

	out.WriteString(renderSectionTitle("◌ Startup Sync"))
	out.WriteString("\n\n")
	out.WriteString(kv("Install Root", m.installRoot))
	out.WriteString("\n")
	out.WriteString(kv("Stage", nonEmptyText(m.startupStage, "preparing")))
	out.WriteString("\n")
	out.WriteString(kv("Pack", nonEmptyText(m.startupPack, "-")))
	out.WriteString("\n")
	if m.startupTotal > 0 {
		out.WriteString(kv("Progress", fmt.Sprintf("%d / %d", m.startupCurrent, m.startupTotal)))
	} else {
		out.WriteString(kv("Progress", "0 / 0"))
	}

	if m.startupDone && m.startupResult != nil {
		out.WriteString("\n\n")
		out.WriteString(chipOK("done"))
		out.WriteString("\n\n")
		out.WriteString(kv("Checked", fmt.Sprintf("%d", m.startupResult.Checked)))
		out.WriteString("\n")
		out.WriteString(kv("Updated", fmt.Sprintf("%d", m.startupResult.Updated)))
		out.WriteString("\n")
		out.WriteString(kv("Skipped", fmt.Sprintf("%d", m.startupResult.Skipped)))
		out.WriteString("\n")
		out.WriteString(kv("Failed", fmt.Sprintf("%d", m.startupResult.Failed)))
	}

	out.WriteString("\n\n")
	out.WriteString(renderHint("[Q] Quit"))
	return out.String()
}

func (m *Model) viewList() string {
	var out strings.Builder

	out.WriteString(appHeader("v1.0.0"))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n")
	out.WriteString(kv("Install Root", m.installRoot))
	out.WriteString("\n\n")

	if m.err != nil {
		out.WriteString(renderErrorBlock(m.err.Error()))
		out.WriteString("\n\n")
	}

	installedRows, availableRows := m.buildSections()

	out.WriteString(m.sectionTitle("● Installed", sectionInstalled))
	out.WriteString("\n\n")
	out.WriteString(renderTableHeader(m.installedTableHeader()))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n")

	if len(installedRows) == 0 {
		out.WriteString(mutedStyle.Render("No installed packs yet."))
	} else {
		start, end := m.visibleRangeFor(len(installedRows), m.installedCursor)
		for i := start; i < end; i++ {
			out.WriteString(m.renderInstalledRow(i, installedRows[i]))
			out.WriteString("\n")
		}
	}

	out.WriteString("\n")
	out.WriteString(m.sectionTitle("○ Available", sectionAvailable))
	out.WriteString("\n\n")
	out.WriteString(renderTableHeader(m.availableTableHeader()))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n")

	if len(availableRows) == 0 {
		out.WriteString(mutedStyle.Render("No additional packs available."))
	} else {
		start, end := m.visibleRangeFor(len(availableRows), m.availableCursor)
		for i := start; i < end; i++ {
			out.WriteString(m.renderAvailableRow(i, availableRows[i]))
			out.WriteString("\n")
		}
	}

	out.WriteString("\n")
	out.WriteString(renderHint("[Tab] Section  [↑/↓] Move  [Enter] Open  [R] Refresh  [Q] Quit"))

	return strings.TrimRight(out.String(), "\n")
}

func (m *Model) sectionTitle(title string, section listSection) string {
	if m.activeSection == section {
		return renderSectionTitle(title + "  " + chipInfo("active"))
	}
	return mutedStyle.Render(title)
}

func (m *Model) installedTableHeader() string {
	nameW := m.nameColumnWidth()
	return "  " +
		padRight("Name", nameW) + "  " +
		padRight("Local", 10) + "  " +
		padRight("Remote", 10) + "  " +
		"Status"
}

func (m *Model) availableTableHeader() string {
	nameW := m.nameColumnWidth()
	return "  " +
		padRight("Name", nameW) + "  " +
		padRight("Remote", 10) + "  " +
		padRight("Size", 10) + "  " +
		"Status"
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

func (m *Model) renderInstalledRow(index int, row packRow) string {
	nameW := m.nameColumnWidth()
	localVersion := nonEmptyText(row.Status.VersionText, "-")
	remoteVersion := nonEmptyText(strings.TrimSpace(row.Pack.Version), "-")

	status := chipInfo("installed")
	if row.UpdateAvailable {
		status = chipWarn("update")
	}

	return renderRowPrefix(
		index == m.installedCursor && m.activeSection == sectionInstalled,
		row.Pack.PackName,
		nameW,
	) + "  " +
		padRight(localVersion, 10) + "  " +
		padRight(remoteVersion, 10) + "  " +
		status
}

func (m *Model) renderAvailableRow(index int, row packRow) string {
	nameW := m.nameColumnWidth()
	remoteVersion := nonEmptyText(strings.TrimSpace(row.Pack.Version), "-")

	return renderRowPrefix(
		index == m.availableCursor && m.activeSection == sectionAvailable,
		row.Pack.PackName,
		nameW,
	) + "  " +
		padRight(remoteVersion, 10) + "  " +
		padRight(formatBytes(row.Pack.SizeBytes), 10) + "  " +
		chipMuted("available")
}

func (m *Model) viewDetails() string {
	row, ok := m.selectedRow()
	if !ok {
		return m.viewList()
	}

	pack := row.Pack
	status := row.Status

	var out strings.Builder
	out.WriteString(appHeader("v1.0.0"))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n\n")

	out.WriteString(renderSectionTitle("◇ Pack Details"))
	out.WriteString("\n\n")

	if status.Installed {
		if row.UpdateAvailable {
			out.WriteString(chipWarn("update"))
		} else {
			out.WriteString(chipInfo("installed"))
		}
	} else {
		out.WriteString(chipMuted("available"))
	}

	out.WriteString("\n\n")
	out.WriteString(kv("Name", pack.PackName))
	out.WriteString("\n")
	out.WriteString(kv("Remote Version", nonEmptyText(strings.TrimSpace(pack.Version), "-")))
	out.WriteString("\n")
	out.WriteString(kv("Local Version", nonEmptyText(status.LocalState.Version, "-")))
	out.WriteString("\n")
	out.WriteString(kv("Status", m.detailStatusText(row)))
	out.WriteString("\n")
	out.WriteString(kv("Pack ID", pack.PackID))
	out.WriteString("\n")
	out.WriteString(kv("Pack Slug", pack.PackSlug))
	out.WriteString("\n")
	out.WriteString(kv("Files", fmt.Sprintf("%d", pack.FileCount)))
	out.WriteString("\n")
	out.WriteString(kv("Size", formatBytes(pack.SizeBytes)))
	out.WriteString("\n")
	out.WriteString(kv("Published At", formatTime(pack.PublishedAt)))
	out.WriteString("\n")
	out.WriteString(kv("Install Dir", status.TargetDir))

	out.WriteString("\n\n")
	actions := "[I] Install"
	if status.Installed {
		actions += "  [U] Update  [X] Reinstall  [D] Delete local"
	}
	actions += "  [Esc] Back  [R] Refresh  [Q] Quit"
	out.WriteString(renderHint(actions))

	return out.String()
}

func (m *Model) detailStatusText(row packRow) string {
	if !row.Status.Installed {
		return "available"
	}
	if row.UpdateAvailable {
		return "update available"
	}
	return "installed"
}

func (m *Model) viewInstallProgress() string {
	return m.viewProgressCommon("↓ Installing")
}

func (m *Model) viewUpdateProgress() string {
	return m.viewProgressCommon("↑ Updating")
}

func (m *Model) viewReinstallProgress() string {
	return m.viewProgressCommon("↻ Reinstalling")
}

func (m *Model) viewProgressCommon(title string) string {
	var out strings.Builder

	out.WriteString(appHeader("v1.0.0"))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n\n")

	out.WriteString(renderSectionTitle(title))
	out.WriteString("\n\n")
	out.WriteString(kv("Stage", nonEmptyText(m.progressStage, "preparing")))
	out.WriteString("\n")
	out.WriteString(kv("File", nonEmptyText(m.progressCurrentFile, "-")))
	out.WriteString("\n")
	out.WriteString(kv("Files", fmt.Sprintf("%d / %d", m.progressFilesDone, m.progressFilesTotal)))
	out.WriteString("\n")
	if m.progressBytesTotal > 0 {
		out.WriteString(kv("Bytes", fmt.Sprintf("%s / %s", formatBytes(m.progressBytesDone), formatBytes(m.progressBytesTotal))))
	} else {
		out.WriteString(kv("Bytes", fmt.Sprintf("%s / -", formatBytes(m.progressBytesDone))))
	}
	out.WriteString("\n")
	out.WriteString(kv("Done", formatPercent(m.progressPercent)))
	out.WriteString("\n\n")
	out.WriteString(renderProgressBar(m.progressPercent, 36))

	if m.progressDone && m.progressErr != nil {
		out.WriteString("\n\n")
		out.WriteString(renderErrorBlock(m.progressErr.Error()))
	}

	out.WriteString("\n\n")
	if m.progressDone {
		if m.progressErr != nil {
			out.WriteString(renderHint("[Esc] Back  [Q] Quit"))
		} else {
			out.WriteString(renderHint("[Enter] Result  [Esc] Back  [Q] Quit"))
		}
	} else {
		out.WriteString(renderHint("[Q] Quit"))
	}

	return out.String()
}

func (m *Model) viewInstallResult() string {
	return m.renderResultScreen(
		m.err,
		m.installResult != nil,
		"No install result",
		"The operation finished without a result payload.",
		"✓ Install Finished",
		resultLinesFromInstall(m.installResult),
	)
}

func (m *Model) viewUpdateResult() string {
	return m.renderResultScreen(
		m.err,
		m.updateResult != nil,
		"No update result",
		"The operation finished without a result payload.",
		"✓ Update Finished",
		resultLinesFromUpdate(m.updateResult),
	)
}

func (m *Model) viewReinstallResult() string {
	return m.renderResultScreen(
		m.err,
		m.reinstallResult != nil,
		"No reinstall result",
		"The operation finished without a result payload.",
		"✓ Reinstall Finished",
		resultLinesFromReinstall(m.reinstallResult),
	)
}

func (m *Model) viewDeleteLocalConfirm() string {
	row, ok := m.selectedRow()
	if !ok {
		return m.viewList()
	}

	var out strings.Builder
	out.WriteString(appHeader("v1.0.0"))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n\n")

	out.WriteString(renderSectionTitle("✕ Delete Local Pack"))
	out.WriteString("\n\n")
	out.WriteString(kv("Pack", row.Pack.PackName))
	out.WriteString("\n")
	out.WriteString(kv("Target Dir", row.Status.TargetDir))
	out.WriteString("\n\n")
	out.WriteString(mutedStyle.Render("This removes only the local installed pack.\nRemote files on the server will stay untouched."))
	out.WriteString("\n\n")
	out.WriteString(renderHint("[Enter] Confirm delete  [Esc] Cancel  [Q] Quit"))

	return out.String()
}

func (m *Model) viewDeleteLocalResult() string {
	return m.renderResultScreen(
		m.err,
		m.deleteResult != nil,
		"No delete result",
		"The operation finished without a result payload.",
		"✓ Local Pack Deleted",
		resultLinesFromDelete(m.deleteResult),
	)
}

func (m *Model) renderResultScreen(opErr error, hasResult bool, emptyTitle, emptyText, sectionTitle string, lines [][2]string) string {
	var out strings.Builder
	out.WriteString(appHeader("v1.0.0"))
	out.WriteString("\n")
	out.WriteString(renderDivider(m.width))
	out.WriteString("\n\n")

	switch {
	case opErr != nil:
		out.WriteString(renderErrorBlock(opErr.Error()))
	case !hasResult:
		out.WriteString(renderInfoBlock(emptyTitle, emptyText))
	default:
		out.WriteString(renderSectionTitle(sectionTitle))
		out.WriteString("\n\n")
		out.WriteString(linesToBody(lines))
	}

	out.WriteString("\n\n")
	out.WriteString(renderHint("[Enter] Back  [Esc] Back  [Q] Quit"))
	return out.String()
}

func linesToBody(lines [][2]string) string {
	var body strings.Builder
	for i, line := range lines {
		body.WriteString(kv(line[0], line[1]))
		if i < len(lines)-1 {
			body.WriteString("\n")
		}
	}
	return body.String()
}

func resultLinesFromInstall(r *install.Result) [][2]string {
	if r == nil {
		return nil
	}
	return installLikeResultLines(
		r.PackName,
		r.Version,
		r.PackID,
		r.TargetDir,
		r.Downloaded,
		r.Skipped,
		r.InstalledAt,
		r.InstalledSize,
	)
}

func resultLinesFromUpdate(r *updatepkg.Result) [][2]string {
	if r == nil {
		return nil
	}
	return [][2]string{
		{"Pack", r.PackName},
		{"Local Version", nonEmptyText(r.LocalVersion, "-")},
		{"Remote Version", r.Version},
		{"Pack ID", r.PackID},
		{"Target Dir", r.TargetDir},
		{"Downloaded", fmt.Sprintf("%d", r.Downloaded)},
		{"Skipped", fmt.Sprintf("%d", r.Skipped)},
		{"Deleted", fmt.Sprintf("%d", r.Deleted)},
		{"Updated At", r.UpdatedAt.Format("2006-01-02 15:04:05")},
		{"Size", formatBytes(r.UpdatedSize)},
	}
}

func resultLinesFromReinstall(r *reinstall.Result) [][2]string {
	if r == nil {
		return nil
	}
	return installLikeResultLines(
		r.PackName,
		r.Version,
		r.PackID,
		r.TargetDir,
		r.Downloaded,
		r.Skipped,
		r.InstalledAt,
		r.InstalledSize,
	)
}

func resultLinesFromDelete(r *removelocal.Result) [][2]string {
	if r == nil {
		return nil
	}
	return [][2]string{
		{"Pack", r.PackName},
		{"Pack ID", r.PackID},
		{"Target Dir", r.TargetDir},
		{"Was Present", humanBool(r.WasPresent)},
		{"Deleted At", r.DeletedAt.Format("2006-01-02 15:04:05")},
	}
}

func installLikeResultLines(
	packName string,
	version string,
	packID string,
	targetDir string,
	downloaded int,
	skipped int,
	at time.Time,
	size int64,
) [][2]string {
	return [][2]string{
		{"Pack", packName},
		{"Version", version},
		{"Pack ID", packID},
		{"Target Dir", targetDir},
		{"Downloaded", fmt.Sprintf("%d", downloaded)},
		{"Skipped", fmt.Sprintf("%d", skipped)},
		{"Installed At", at.Format("2006-01-02 15:04:05")},
		{"Size", formatBytes(size)},
	}
}

func loadServerPacksCmd(cfg appconfig.Config) tea.Cmd {
	return makeStoreCmd(
		cfg,
		func(ctx context.Context, store *storage.Client) ([]serverpacks.Pack, error) {
			return serverpacks.ListFromIndex(ctx, store)
		},
		func(result []serverpacks.Pack, err error) tea.Msg {
			return serverPacksLoadedMsg{packs: result, err: err}
		},
	)
}

func startupSyncCmd(cfg appconfig.Config, installRoot string, statusCh chan autoupdate.StatusUpdate) tea.Cmd {
	return func() tea.Msg {
		defer close(statusCh)

		ctx := context.Background()

		store, err := storage.New(ctx, cfg)
		if err != nil {
			res := autoupdate.Result{
				Failed: 1,
				Items:  []autoupdate.ItemResult{{PackName: "startup", ErrText: err.Error()}},
			}
			return startupSyncMsg{
				update: autoupdate.StatusUpdate{
					Stage:   "Creating storage client",
					Done:    true,
					Result:  &res,
					ErrText: err.Error(),
				},
			}
		}

		res := autoupdate.Run(ctx, store, installRoot, statusCh)
		return startupSyncMsg{
			update: autoupdate.StatusUpdate{
				Stage:  "Startup sync finished",
				Done:   true,
				Result: &res,
			},
		}
	}
}

func installCmd(cfg appconfig.Config, pack serverpacks.Pack, installRoot string, progressCh chan install.ProgressUpdate) tea.Cmd {
	return func() tea.Msg {
		defer close(progressCh)
		return makeStoreCmd(
			cfg,
			func(ctx context.Context, store *storage.Client) (install.Result, error) {
				return install.InstallPack(ctx, store, pack, installRoot, progressCh)
			},
			func(result install.Result, err error) tea.Msg {
				return installDoneMsg{result: result, err: err}
			},
		)()
	}
}

func updateCmd(cfg appconfig.Config, pack serverpacks.Pack, installRoot string, progressCh chan updatepkg.ProgressUpdate) tea.Cmd {
	return func() tea.Msg {
		defer close(progressCh)
		return makeStoreCmd(
			cfg,
			func(ctx context.Context, store *storage.Client) (updatepkg.Result, error) {
				return updatepkg.Pack(ctx, store, pack, installRoot, progressCh)
			},
			func(result updatepkg.Result, err error) tea.Msg {
				return updateDoneMsg{result: result, err: err}
			},
		)()
	}
}

func reinstallCmd(cfg appconfig.Config, pack serverpacks.Pack, installRoot string, progressCh chan reinstall.ProgressUpdate) tea.Cmd {
	return func() tea.Msg {
		defer close(progressCh)
		return makeStoreCmd(
			cfg,
			func(ctx context.Context, store *storage.Client) (reinstall.Result, error) {
				return reinstall.Pack(ctx, store, pack, installRoot, progressCh)
			},
			func(result reinstall.Result, err error) tea.Msg {
				return reinstallDoneMsg{result: result, err: err}
			},
		)()
	}
}

func deleteLocalCmd(installRoot string, pack serverpacks.Pack) tea.Cmd {
	return func() tea.Msg {
		result, err := removelocal.Pack(installRoot, pack)
		return deleteLocalDoneMsg{result: result, err: err}
	}
}

func makeStoreCmd[T any](cfg appconfig.Config, fn func(context.Context, *storage.Client) (T, error), wrap func(T, error) tea.Msg) tea.Cmd {
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

func listenStartupSyncCmd(ch <-chan autoupdate.StatusUpdate) tea.Cmd {
	return listenChan(ch, func(update autoupdate.StatusUpdate, ch <-chan autoupdate.StatusUpdate) tea.Msg {
		return startupSyncMsg{update: update, ch: ch}
	})
}

func listenInstallProgressCmd(ch <-chan install.ProgressUpdate) tea.Cmd {
	return listenChan(ch, func(update install.ProgressUpdate, ch <-chan install.ProgressUpdate) tea.Msg {
		return installProgressMsg{update: update, ch: ch}
	})
}

func listenUpdateProgressCmd(ch <-chan updatepkg.ProgressUpdate) tea.Cmd {
	return listenChan(ch, func(update updatepkg.ProgressUpdate, ch <-chan updatepkg.ProgressUpdate) tea.Msg {
		return updateProgressMsg{update: update, ch: ch}
	})
}

func listenReinstallProgressCmd(ch <-chan reinstall.ProgressUpdate) tea.Cmd {
	return listenChan(ch, func(update reinstall.ProgressUpdate, ch <-chan reinstall.ProgressUpdate) tea.Msg {
		return reinstallProgressMsg{update: update, ch: ch}
	})
}

func listenChan[T any](ch <-chan T, wrap func(T, <-chan T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-ch
		if !ok {
			return nil
		}
		return wrap(update, ch)
	}
}

func (m *Model) beginOperation(next screen, action tea.Cmd, listener tea.Cmd) (tea.Model, tea.Cmd) {
	m.resetProgress()
	m.screen = next
	return m, tea.Batch(action, listener)
}

func (m *Model) handleProgressMessage(
	next tea.Cmd,
	stage string,
	currentFile string,
	filesDone int,
	filesTotal int,
	bytesDone int64,
	bytesTotal int64,
	done bool,
	err error,
) (tea.Model, tea.Cmd) {
	m.applyProgress(stage, currentFile, filesDone, filesTotal, bytesDone, bytesTotal, done, err)

	if done {
		if err != nil {
			m.err = err
		}
		return m, nil
	}

	return m, next
}

func (m *Model) finishOperation(
	nextScreen screen,
	opErr error,
	onError func(error),
	onSuccess func(),
) (tea.Model, tea.Cmd) {
	if opErr != nil {
		m.err = opErr
		onError(opErr)
	} else {
		onSuccess()
	}

	m.progressDone = true
	m.screen = nextScreen
	return m, tea.Batch(loadServerPacksCmd(m.cfg))
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

func (m *Model) applyProgress(stage, currentFile string, filesDone, filesTotal int, bytesDone, bytesTotal int64, done bool, err error) {
	m.progressStage = stage
	m.progressCurrentFile = currentFile
	m.progressFilesDone = filesDone
	m.progressFilesTotal = filesTotal
	m.progressBytesDone = bytesDone
	m.progressBytesTotal = bytesTotal
	m.progressDone = done
	m.progressErr = err

	if bytesTotal > 0 {
		m.progressPercent = (float64(bytesDone) / float64(bytesTotal)) * 100
		if m.progressPercent > 100 {
			m.progressPercent = 100
		}
	} else if filesTotal > 0 {
		m.progressPercent = (float64(filesDone) / float64(filesTotal)) * 100
		if m.progressPercent > 100 {
			m.progressPercent = 100
		}
	} else {
		m.progressPercent = 0
	}
}

func inspectInstalledPack(root string, pack serverpacks.Pack) installedStatus {
	targetDir := targetInstallDir(root, pack.PackName)

	state, exists, err := clientstate.Read(targetDir)
	if err != nil || !exists {
		return installedStatus{
			Installed:   false,
			TargetDir:   targetDir,
			VersionText: "-",
		}
	}

	if strings.TrimSpace(state.PackID) != "" && strings.TrimSpace(pack.PackID) != "" && state.PackID != pack.PackID {
		return installedStatus{
			Installed:   false,
			TargetDir:   targetDir,
			VersionText: "-",
		}
	}

	versionText := state.Version
	if strings.TrimSpace(versionText) == "" {
		versionText = "-"
	}

	return installedStatus{
		Installed:   true,
		TargetDir:   targetDir,
		LocalState:  state,
		VersionText: versionText,
	}
}

func (m *Model) buildSections() ([]packRow, []packRow) {
	installed := make([]packRow, 0)
	available := make([]packRow, 0)

	for _, pack := range m.packs {
		status := inspectInstalledPack(m.installRoot, pack)
		row := packRow{
			Pack:            pack,
			Status:          status,
			UpdateAvailable: packNeedsUpdate(status, pack),
		}

		if status.Installed {
			installed = append(installed, row)
		} else {
			available = append(available, row)
		}
	}

	return installed, available
}

func (m *Model) clampCursors() {
	installed, available := m.buildSections()

	if len(installed) == 0 {
		m.installedCursor = 0
	} else {
		if m.installedCursor < 0 {
			m.installedCursor = 0
		}
		if m.installedCursor >= len(installed) {
			m.installedCursor = len(installed) - 1
		}
	}

	if len(available) == 0 {
		m.availableCursor = 0
	} else {
		if m.availableCursor < 0 {
			m.availableCursor = 0
		}
		if m.availableCursor >= len(available) {
			m.availableCursor = len(available) - 1
		}
	}

	if m.activeSection == sectionInstalled && len(installed) == 0 && len(available) > 0 {
		m.activeSection = sectionAvailable
	}
	if m.activeSection == sectionAvailable && len(available) == 0 && len(installed) > 0 {
		m.activeSection = sectionInstalled
	}
}

func (m *Model) switchSection() {
	installed, available := m.buildSections()

	if m.activeSection == sectionInstalled {
		if len(available) > 0 {
			m.activeSection = sectionAvailable
		}
		return
	}

	if len(installed) > 0 {
		m.activeSection = sectionInstalled
	}
}

func (m *Model) moveCursor(delta int) {
	installed, available := m.buildSections()

	if m.activeSection == sectionInstalled {
		if len(installed) == 0 {
			return
		}
		m.installedCursor += delta
		if m.installedCursor < 0 {
			m.installedCursor = 0
		}
		if m.installedCursor >= len(installed) {
			m.installedCursor = len(installed) - 1
		}
		return
	}

	if len(available) == 0 {
		return
	}
	m.availableCursor += delta
	if m.availableCursor < 0 {
		m.availableCursor = 0
	}
	if m.availableCursor >= len(available) {
		m.availableCursor = len(available) - 1
	}
}

func (m *Model) selectedRow() (packRow, bool) {
	installed, available := m.buildSections()

	if m.activeSection == sectionInstalled {
		if len(installed) == 0 || m.installedCursor < 0 || m.installedCursor >= len(installed) {
			return packRow{}, false
		}
		return installed[m.installedCursor], true
	}

	if len(available) == 0 || m.availableCursor < 0 || m.availableCursor >= len(available) {
		return packRow{}, false
	}
	return available[m.availableCursor], true
}

func (m *Model) visibleRangeFor(total, cursor int) (int, int) {
	pageSize := m.listPageSize()
	if total <= 0 {
		return 0, 0
	}

	start := 0
	if cursor >= pageSize {
		start = cursor - pageSize + 1
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

func (m *Model) listPageSize() int {
	if m.height <= 0 {
		return 12
	}
	return intMax(6, m.height-14)
}

func (m *Model) nameColumnWidth() int {
	w := 26
	if m.width >= 100 {
		w = 34
	}
	if m.width >= 120 {
		w = 42
	}
	return w
}

func packNeedsUpdate(status installedStatus, pack serverpacks.Pack) bool {
	if !status.Installed {
		return false
	}

	localVersion := strings.TrimSpace(status.LocalState.Version)
	remoteVersion := strings.TrimSpace(pack.Version)
	localManifest := strings.TrimSpace(status.LocalState.ManifestKey)
	remoteManifest := strings.TrimSpace(pack.ManifestKey)

	return localVersion != remoteVersion || localManifest != remoteManifest
}

func targetInstallDir(root, packName string) string {
	name := strings.TrimSpace(packName)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" {
		name = "pack"
	}
	return filepath.Join(root, name)
}

func nonEmptyText(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
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

func isQuitKey(s string) bool      { return s == "q" || s == "й" }
func isRefreshKey(s string) bool   { return s == "r" || s == "к" }
func isInstallKey(s string) bool   { return s == "i" || s == "ш" }
func isUpdateKey(s string) bool    { return s == "u" || s == "г" }
func isReinstallKey(s string) bool { return s == "x" || s == "ч" }
func isDeleteKey(s string) bool    { return s == "d" || s == "в" }
func isUpKey(s string) bool        { return s == "up" || s == "k" || s == "л" }
func isDownKey(s string) bool      { return s == "down" || s == "j" || s == "о" }

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
