package output

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	baselineMinWidth                   = 76
	baselineMinHeight                  = 22
	baselineGap                        = 2
	baselineRepositoryPickerMaxVisible = 10
	baselineEscapeSequenceWait         = 40 * time.Millisecond
)

// BaselineWorkspaceOutcome describes why an interactive workspace returned.
type BaselineWorkspaceOutcome string

const (
	BaselineWorkspaceBack      BaselineWorkspaceOutcome = "back"
	BaselineWorkspaceQuit      BaselineWorkspaceOutcome = "quit"
	BaselineWorkspaceCancelled BaselineWorkspaceOutcome = "cancelled"
)

// ErrBaselineCancelled is returned when Ctrl+C cancels repository selection.
var ErrBaselineCancelled = errors.New("baseline selection cancelled")

// BaselineRepositoryOption is one repository already present in the stored
// baseline registry. ID is the exact value accepted by --repo.
type BaselineRepositoryOption struct {
	ID          string
	DisplayName string
	Commit      string
}

// BaselineWorkspace is a read-only terminal view over one normalized baseline.
type BaselineWorkspace struct {
	catalog                   *BaselineCatalog
	blueprint                 map[string]any
	repositoryID              string
	commit                    string
	color                     bool
	includeUncontrolledAssets bool
}

// NewBaselineWorkspace validates and normalizes one repository's artifact.
// The renderer never retains the caller's mutable top-level maps.
func NewBaselineWorkspace(
	raw, blueprint map[string]any,
	repositoryID, commit string,
) (*BaselineWorkspace, error) {
	catalog, err := ParseBaselineCatalog(raw)
	if err != nil {
		return nil, err
	}
	return &BaselineWorkspace{
		catalog:      catalog,
		blueprint:    cloneBaselineMap(blueprint),
		repositoryID: sanitizeBaselineText(repositoryID, false),
		commit:       sanitizeBaselineText(commit, false),
		color:        baselineColorEnabled(os.Stdout),
	}, nil
}

// BaselineTerminalInteractive reports whether the workspace may safely prompt.
func BaselineTerminalInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// StaticSummary renders a deterministic, non-interactive summary without ANSI
// escapes. It does not inspect the current repository or refresh the baseline.
func (w *BaselineWorkspace) StaticSummary() string {
	style := baselineStyle{}
	rows := w.repositoryHeader(style, 100, true)
	return strings.Join(rows, "\n") + "\n"
}

// Browse opens the raw-terminal asset workspace. The caller decides whether a
// Back outcome reopens repository selection and whether Quit/Cancelled print
// any final message.
func (w *BaselineWorkspace) Browse() (BaselineWorkspaceOutcome, error) {
	if !BaselineTerminalInteractive() {
		return BaselineWorkspaceQuit, errors.New("baseline workspace requires terminal stdin and stdout")
	}

	stdinFD := int(os.Stdin.Fd())
	restore, err := enterBaselineRawTerminal(stdinFD, func() {
		_, _ = io.WriteString(os.Stdout, "\033[?25h\033[?1049l")
	})
	if err != nil {
		return BaselineWorkspaceQuit, fmt.Errorf("entering raw terminal mode: %w", err)
	}
	defer restore()

	if _, err := io.WriteString(os.Stdout, "\033[?1049h\033[?25l"); err != nil {
		return BaselineWorkspaceQuit, err
	}

	w.color = baselineColorEnabled(os.Stdout)
	return w.runBaselineWorkspace(bufio.NewReader(os.Stdin), os.Stdout, func() (int, int) {
		width, height, sizeErr := term.GetSize(int(os.Stdout.Fd()))
		if sizeErr != nil || width <= 0 || height <= 0 {
			return 120, 32
		}
		return width, height
	}, func() (bool, error) {
		return baselineWaitForInput(stdinFD, baselineEscapeSequenceWait)
	})
}

// PickBaselineRepository selects only from repositories already in the stored
// artifact. Up/Down clamp at the ends; Enter/Right opens; Escape/Q exits.
func PickBaselineRepository(
	options []BaselineRepositoryOption,
	selected int,
) (index int, opened bool, err error) {
	if len(options) == 0 {
		return 0, false, errors.New("repository picker requires at least one stored repository")
	}
	selected = clampBaseline(selected, 0, len(options)-1)
	if !BaselineTerminalInteractive() {
		return selected, false, errors.New("repository picker requires terminal stdin and stdout")
	}

	stdinFD := int(os.Stdin.Fd())
	restore, err := enterBaselineRawTerminal(stdinFD, nil)
	if err != nil {
		return selected, false, fmt.Errorf("entering raw terminal mode: %w", err)
	}
	defer restore()

	return pickBaselineRepositoryIO(
		bufio.NewReader(os.Stdin),
		os.Stdout,
		options,
		selected,
		baselineColorEnabled(os.Stdout),
		func() (bool, error) {
			return baselineWaitForInput(stdinFD, baselineEscapeSequenceWait)
		},
	)
}

// enterBaselineRawTerminal restores the terminal before this process exits for
// an interactive terminating signal. Go defers are not run for those signals.
func enterBaselineRawTerminal(stdinFD int, cleanup func()) (func(), error) {
	oldState, err := term.MakeRaw(stdinFD)
	if err != nil {
		return nil, err
	}

	var once sync.Once
	restore := func() {
		once.Do(func() {
			if cleanup != nil {
				cleanup()
			}
			_ = term.Restore(stdinFD, oldState)
		})
	}

	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-signals:
			restore()
			signal.Stop(signals)
			signal.Reset(sig)
			_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
		case <-done:
		}
	}()

	return func() {
		close(done)
		signal.Stop(signals)
		restore()
	}, nil
}

type baselineInputWaiter func() (bool, error)

func pickBaselineRepositoryIO(
	reader *bufio.Reader,
	writer io.Writer,
	options []BaselineRepositoryOption,
	selected int,
	color bool,
	waiters ...baselineInputWaiter,
) (int, bool, error) {
	if len(options) == 0 {
		return 0, false, errors.New("repository picker requires at least one stored repository")
	}
	selected = clampBaseline(selected, 0, len(options)-1)
	renderedLines := 0
	for {
		if renderedLines > 0 {
			clearBaselineLines(writer, renderedLines)
		}
		frame := renderBaselineRepositoryPicker(options, selected, baselineStyle{enabled: color})
		if _, err := io.WriteString(writer, frame); err != nil {
			return selected, false, err
		}
		renderedLines = strings.Count(frame, "\n")

		key, err := readBaselineKey(reader, waiters...)
		if err != nil {
			return selected, false, err
		}
		switch key.kind {
		case baselineKeyUp:
			selected = max(0, selected-1)
		case baselineKeyDown:
			selected = min(len(options)-1, selected+1)
		case baselineKeyEnter, baselineKeyRight:
			clearBaselineLines(writer, renderedLines)
			return selected, true, nil
		case baselineKeyEscape, baselineKeyQuit:
			clearBaselineLines(writer, renderedLines)
			return selected, false, nil
		case baselineKeyCancel:
			clearBaselineLines(writer, renderedLines)
			return selected, false, ErrBaselineCancelled
		}
	}
}

func renderBaselineRepositoryPicker(
	options []BaselineRepositoryOption,
	selected int,
	style baselineStyle,
) string {
	if len(options) == 0 {
		return ""
	}
	selected = clampBaseline(selected, 0, len(options)-1)
	visible := min(baselineRepositoryPickerMaxVisible, len(options))
	start := max(0, min(selected-visible/2, len(options)-visible))
	end := start + visible

	nameWidth := 0
	for _, option := range options[start:end] {
		name := sanitizeBaselineText(option.DisplayName, false)
		if name == "" {
			name = sanitizeBaselineText(option.ID, false)
		}
		nameWidth = max(nameWidth, visibleLen(name))
	}
	rowWidth := nameWidth + 4
	for _, option := range options[start:end] {
		if option.Commit != "" {
			rowWidth = max(rowWidth, nameWidth+2+min(12, visibleLen(option.Commit))+4)
		}
	}

	var out strings.Builder
	out.WriteString(style.bold("Select a repository"))
	out.WriteString("\r\n\r\n")
	for index := start; index < end; index++ {
		option := options[index]
		name := sanitizeBaselineText(option.DisplayName, false)
		if name == "" {
			name = sanitizeBaselineText(option.ID, false)
		}
		marker := " "
		if index == selected {
			marker = "›"
		}
		row := "  " + marker + " " + baselinePadRight(name, nameWidth)
		if option.Commit != "" {
			row += "  " + baselineShortCommit(option.Commit)
		}
		row = baselineFit(row, rowWidth)
		if index == selected {
			row = style.highlight(row)
		}
		out.WriteString(row)
		out.WriteString("\r\n")
	}
	if len(options) > visible {
		out.WriteString(style.dim(fmt.Sprintf(
			"    %d–%d of %d · use ↑↓ to scroll",
			start+1,
			end,
			len(options),
		)))
		out.WriteString("\r\n")
	}
	return out.String()
}

func clearBaselineLines(writer io.Writer, count int) {
	if count <= 0 {
		return
	}
	for range count {
		_, _ = io.WriteString(writer, "\033[1A\r\033[2K")
	}
}

type baselineFocus uint8

const (
	baselineFocusTypes baselineFocus = iota
	baselineFocusAssets
	baselineFocusDetail
	baselineFocusControl
)

type baselineWorkspaceState struct {
	kindIndex           int
	assetIndex          int
	focus               baselineFocus
	query               string
	draftQuery          string
	filtering           bool
	detailTarget        baselineDetailTarget
	openedControlID     string
	detailScroll        int
	controlDetailScroll int
}

func initialBaselineWorkspaceState() baselineWorkspaceState {
	return baselineWorkspaceState{focus: baselineFocusTypes}
}

type baselineTargetKind uint8

const (
	baselineTargetNone baselineTargetKind = iota
	baselineTargetControl
	baselineTargetAsset
)

type baselineDetailTarget struct {
	kind  baselineTargetKind
	id    string
	rowID string
}

type baselineKeyKind uint8

const (
	baselineKeyUnknown baselineKeyKind = iota
	baselineKeyUp
	baselineKeyDown
	baselineKeyLeft
	baselineKeyRight
	baselineKeyEnter
	baselineKeyEscape
	baselineKeyPageUp
	baselineKeyPageDown
	baselineKeyFilter
	baselineKeyClear
	baselineKeyQuit
	baselineKeyCancel
	baselineKeyBackspace
	baselineKeyCharacter
)

type baselineKey struct {
	kind baselineKeyKind
	rune rune
}

func readBaselineKey(reader *bufio.Reader, waiters ...baselineInputWaiter) (baselineKey, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return baselineKey{}, err
	}
	switch first {
	case 3:
		return baselineKey{kind: baselineKeyCancel}, nil
	case '\r', '\n':
		return baselineKey{kind: baselineKeyEnter}, nil
	case 8, 127:
		return baselineKey{kind: baselineKeyBackspace}, nil
	case 27:
		ready, waitErr := baselineInputReady(reader, waiters)
		if waitErr != nil {
			return baselineKey{}, waitErr
		}
		if !ready {
			return baselineKey{kind: baselineKeyEscape}, nil
		}
		second, secondErr := reader.ReadByte()
		if secondErr != nil || second != '[' {
			return baselineKey{kind: baselineKeyEscape}, nil
		}
		// Once CSI has started, read it to completion. A slow terminal may
		// deliver its final byte after the Escape-disambiguation timeout; that
		// must not be treated as a standalone Escape/back-navigation request.
		third, thirdErr := reader.ReadByte()
		if thirdErr != nil {
			return baselineKey{kind: baselineKeyEscape}, nil
		}
		switch third {
		case 'A':
			return baselineKey{kind: baselineKeyUp}, nil
		case 'B':
			return baselineKey{kind: baselineKeyDown}, nil
		case 'C':
			return baselineKey{kind: baselineKeyRight}, nil
		case 'D':
			return baselineKey{kind: baselineKeyLeft}, nil
		case '5', '6':
			trailing, trailingErr := reader.ReadByte()
			if trailingErr != nil || trailing != '~' {
				return baselineKey{kind: baselineKeyUnknown}, nil
			}
			if third == '5' {
				return baselineKey{kind: baselineKeyPageUp}, nil
			}
			return baselineKey{kind: baselineKeyPageDown}, nil
		default:
			return baselineKey{kind: baselineKeyUnknown}, nil
		}
	case '/':
		return baselineKey{kind: baselineKeyFilter, rune: '/'}, nil
	case 'c', 'C':
		return baselineKey{kind: baselineKeyClear, rune: rune(first)}, nil
	case 'q', 'Q':
		return baselineKey{kind: baselineKeyQuit, rune: rune(first)}, nil
	default:
		if first < utf8.RuneSelf {
			if first >= 32 {
				return baselineKey{kind: baselineKeyCharacter, rune: rune(first)}, nil
			}
			return baselineKey{kind: baselineKeyUnknown}, nil
		}
		if err := reader.UnreadByte(); err != nil {
			return baselineKey{}, err
		}
		r, _, err := reader.ReadRune()
		if err != nil {
			return baselineKey{}, err
		}
		return baselineKey{kind: baselineKeyCharacter, rune: r}, nil
	}
}

func baselineInputReady(reader *bufio.Reader, waiters []baselineInputWaiter) (bool, error) {
	if reader.Buffered() > 0 {
		return true, nil
	}
	if len(waiters) == 0 || waiters[0] == nil {
		return false, nil
	}
	return waiters[0]()
}

type baselineTransition uint8

const (
	baselineTransitionContinue baselineTransition = iota
	baselineTransitionBack
	baselineTransitionQuit
	baselineTransitionCancelled
)

func (w *BaselineWorkspace) runBaselineWorkspace(
	reader *bufio.Reader,
	writer io.Writer,
	size func() (int, int),
	waiters ...baselineInputWaiter,
) (BaselineWorkspaceOutcome, error) {
	state := initialBaselineWorkspaceState()
	for {
		var err error
		state, err = w.normalizeBaselineState(state)
		if err != nil {
			return BaselineWorkspaceQuit, err
		}
		width, height := size()
		frame := w.renderBaselineFrame(state, width, height)
		if _, err := io.WriteString(writer, "\033[H\033[2J"+frame); err != nil {
			return BaselineWorkspaceQuit, err
		}

		key, err := readBaselineKey(reader, waiters...)
		if err != nil {
			return BaselineWorkspaceQuit, err
		}
		var transition baselineTransition
		state, transition, err = w.reduceBaselineState(state, key)
		if err != nil {
			return BaselineWorkspaceQuit, err
		}
		switch transition {
		case baselineTransitionBack:
			return BaselineWorkspaceBack, nil
		case baselineTransitionQuit:
			return BaselineWorkspaceQuit, nil
		case baselineTransitionCancelled:
			return BaselineWorkspaceCancelled, nil
		}
	}
}

func (w *BaselineWorkspace) reduceBaselineState(
	state baselineWorkspaceState,
	key baselineKey,
) (baselineWorkspaceState, baselineTransition, error) {
	state, err := w.normalizeBaselineState(state)
	if err != nil {
		return state, baselineTransitionContinue, err
	}

	if key.kind == baselineKeyCancel {
		return state, baselineTransitionCancelled, nil
	}
	if state.filtering {
		switch key.kind {
		case baselineKeyEnter:
			state.query = state.draftQuery
			state.filtering = false
		case baselineKeyEscape:
			state.draftQuery = state.query
			state.filtering = false
		case baselineKeyBackspace:
			state.draftQuery = trimLastBaselineRune(state.draftQuery)
			state.assetIndex = 0
			state.detailTarget = baselineDetailTarget{}
			state.detailScroll = 0
		case baselineKeyCharacter, baselineKeyFilter, baselineKeyClear, baselineKeyQuit:
			if key.rune != 0 && !unicode.IsControl(key.rune) {
				state.draftQuery += string(key.rune)
				state.assetIndex = 0
				state.detailTarget = baselineDetailTarget{}
				state.detailScroll = 0
			}
		}
		state, err = w.normalizeBaselineState(state)
		return state, baselineTransitionContinue, err
	}

	switch key.kind {
	case baselineKeyQuit:
		return state, baselineTransitionQuit, nil
	case baselineKeyFilter:
		state.draftQuery = state.query
		state.filtering = true
		return state, baselineTransitionContinue, nil
	case baselineKeyClear:
		if state.query != "" {
			state.query = ""
			state.draftQuery = ""
			state.assetIndex = 0
			state.detailTarget = baselineDetailTarget{}
			state.detailScroll = 0
		}
		return w.normalizedTransition(state)
	case baselineKeyEscape, baselineKeyLeft:
		switch state.focus {
		case baselineFocusControl:
			state.focus = baselineFocusDetail
			state.openedControlID = ""
			state.controlDetailScroll = 0
		case baselineFocusDetail:
			state.focus = baselineFocusAssets
		case baselineFocusAssets:
			state.focus = baselineFocusTypes
		case baselineFocusTypes:
			return state, baselineTransitionBack, nil
		}
		return w.normalizedTransition(state)
	case baselineKeyPageUp:
		if state.focus == baselineFocusControl {
			state.controlDetailScroll = max(0, state.controlDetailScroll-5)
		} else {
			state.detailScroll = max(0, state.detailScroll-5)
		}
		return state, baselineTransitionContinue, nil
	case baselineKeyPageDown:
		if state.focus == baselineFocusControl {
			state.controlDetailScroll += 5
		} else {
			state.detailScroll += 5
		}
		return state, baselineTransitionContinue, nil
	}

	kind := baselineAssetKinds[state.kindIndex]
	assets, err := w.filteredBaselineAssets(kind, state.activeQuery())
	if err != nil {
		return state, baselineTransitionContinue, err
	}
	var current BaselineDiscovery
	if len(assets) > 0 {
		current = assets[state.assetIndex]
	}
	targets := w.baselineDetailTargets(current)

	switch key.kind {
	case baselineKeyEnter:
		switch state.focus {
		case baselineFocusTypes:
			state.focus = baselineFocusAssets
		case baselineFocusAssets:
			if len(targets) > 0 {
				state.focus = baselineFocusDetail
				state.detailTarget = targets[0]
			}
		case baselineFocusDetail:
			return w.activateBaselineDetailTarget(state)
		}
	case baselineKeyRight:
		switch state.focus {
		case baselineFocusTypes:
			state.focus = baselineFocusAssets
		case baselineFocusAssets:
			if len(targets) > 0 {
				state.focus = baselineFocusDetail
				state.detailTarget = targets[0]
			}
		case baselineFocusDetail:
			if state.detailTarget.kind == baselineTargetControl {
				state.openedControlID = state.detailTarget.id
				state.controlDetailScroll = 0
				state.focus = baselineFocusControl
			}
		}
	case baselineKeyUp:
		switch state.focus {
		case baselineFocusTypes:
			if state.kindIndex > 0 {
				state.kindIndex--
				state.resetBaselineAsset()
			}
		case baselineFocusAssets:
			if state.assetIndex == 0 {
				state.focus = baselineFocusTypes
			} else {
				state.assetIndex--
				state.resetBaselineDetail()
			}
		case baselineFocusDetail:
			state.detailTarget = adjacentBaselineTarget(targets, state.detailTarget, -1)
		case baselineFocusControl:
			state.detailTarget = adjacentBaselineControlTarget(targets, state.detailTarget, -1)
			state.openedControlID = state.detailTarget.id
			state.controlDetailScroll = 0
		}
	case baselineKeyDown:
		switch state.focus {
		case baselineFocusTypes:
			if state.kindIndex < len(baselineAssetKinds)-1 {
				state.kindIndex++
				state.resetBaselineAsset()
			}
		case baselineFocusAssets:
			if len(assets) > 0 && state.assetIndex < len(assets)-1 {
				state.assetIndex++
				state.resetBaselineDetail()
			}
		case baselineFocusDetail:
			state.detailTarget = adjacentBaselineTarget(targets, state.detailTarget, 1)
		case baselineFocusControl:
			state.detailTarget = adjacentBaselineControlTarget(targets, state.detailTarget, 1)
			state.openedControlID = state.detailTarget.id
			state.controlDetailScroll = 0
		}
	}
	return w.normalizedTransition(state)
}

func (w *BaselineWorkspace) activateBaselineDetailTarget(
	state baselineWorkspaceState,
) (baselineWorkspaceState, baselineTransition, error) {
	switch state.detailTarget.kind {
	case baselineTargetControl:
		state.openedControlID = state.detailTarget.id
		state.controlDetailScroll = 0
		state.focus = baselineFocusControl
		return w.normalizedTransition(state)
	case baselineTargetAsset:
		asset, ok := w.catalog.Asset(state.detailTarget.id)
		if !ok {
			return state, baselineTransitionContinue, nil
		}
		kindIndex := baselineKindIndex(asset.Kind)
		if kindIndex < 0 {
			return state, baselineTransitionContinue, nil
		}
		state.kindIndex = kindIndex
		state.query = ""
		state.draftQuery = ""
		state.assetIndex = 0
		state.focus = baselineFocusAssets
		state.resetBaselineDetail()
		assets, err := w.filteredBaselineAssets(asset.Kind, "")
		if err != nil {
			return state, baselineTransitionContinue, err
		}
		for index, candidate := range assets {
			if candidate.ID == asset.ID {
				state.assetIndex = index
				break
			}
		}
	}
	return w.normalizedTransition(state)
}

func (w *BaselineWorkspace) normalizedTransition(
	state baselineWorkspaceState,
) (baselineWorkspaceState, baselineTransition, error) {
	normalized, err := w.normalizeBaselineState(state)
	return normalized, baselineTransitionContinue, err
}

func (w *BaselineWorkspace) normalizeBaselineState(
	state baselineWorkspaceState,
) (baselineWorkspaceState, error) {
	state.kindIndex = clampBaseline(state.kindIndex, 0, len(baselineAssetKinds)-1)
	assets, err := w.filteredBaselineAssets(
		baselineAssetKinds[state.kindIndex],
		state.activeQuery(),
	)
	if err != nil {
		return state, err
	}
	if len(assets) == 0 {
		state.assetIndex = 0
		state.detailTarget = baselineDetailTarget{}
		state.openedControlID = ""
		if state.focus == baselineFocusDetail || state.focus == baselineFocusControl {
			state.focus = baselineFocusAssets
		}
		return state, nil
	}
	state.assetIndex = clampBaseline(state.assetIndex, 0, len(assets)-1)
	targets := w.baselineDetailTargets(assets[state.assetIndex])
	if !containsBaselineTarget(targets, state.detailTarget) {
		if len(targets) > 0 {
			state.detailTarget = targets[0]
		} else {
			state.detailTarget = baselineDetailTarget{}
		}
	}
	if state.focus == baselineFocusDetail && len(targets) == 0 {
		state.focus = baselineFocusAssets
	}
	if state.focus == baselineFocusControl {
		if state.detailTarget.kind != baselineTargetControl {
			state.detailTarget = adjacentBaselineControlTarget(targets, state.detailTarget, 0)
		}
		if state.detailTarget.kind != baselineTargetControl {
			state.focus = baselineFocusDetail
			state.openedControlID = ""
		} else {
			state.openedControlID = state.detailTarget.id
		}
	}
	return state, nil
}

func (state *baselineWorkspaceState) resetBaselineAsset() {
	state.assetIndex = 0
	state.resetBaselineDetail()
}

func (state *baselineWorkspaceState) resetBaselineDetail() {
	state.detailTarget = baselineDetailTarget{}
	state.openedControlID = ""
	state.detailScroll = 0
	state.controlDetailScroll = 0
}

func (state baselineWorkspaceState) activeQuery() string {
	if state.filtering {
		return state.draftQuery
	}
	return state.query
}

var baselineAssetKindLabels = map[string]string{
	"endpoint": "Endpoints",
	"object":   "Objects",
	"field":    "Fields",
	"code":     "Code",
}

func baselineKindIndex(kind string) int {
	for index, candidate := range baselineAssetKinds {
		if candidate == kind {
			return index
		}
	}
	return -1
}

func (w *BaselineWorkspace) filteredBaselineAssets(
	kind, query string,
) ([]BaselineDiscovery, error) {
	assets, err := w.baselineAssets(kind)
	if err != nil || query == "" {
		return assets, err
	}
	needle := strings.ToLower(query)
	filtered := make([]BaselineDiscovery, 0, len(assets))
	for _, asset := range assets {
		haystack := strings.ToLower(strings.Join([]string{
			asset.Name,
			asset.ID,
			asset.Location,
			asset.Description,
		}, " "))
		if strings.Contains(haystack, needle) {
			filtered = append(filtered, asset)
		}
	}
	return filtered, nil
}

func (w *BaselineWorkspace) baselineAssets(kind string) ([]BaselineDiscovery, error) {
	if w.includeUncontrolledAssets {
		return w.catalog.Discoveries(kind)
	}
	return w.catalog.ReviewableAssets(kind)
}

func (w *BaselineWorkspace) baselineAssetCounts() map[string]int {
	counts := make(map[string]int, len(baselineAssetKinds))
	for _, kind := range baselineAssetKinds {
		assets, _ := w.baselineAssets(kind)
		counts[kind] = len(assets)
	}
	return counts
}

func (w *BaselineWorkspace) baselineDetailTargets(
	asset BaselineDiscovery,
) []baselineDetailTarget {
	if asset.ID == "" {
		return nil
	}
	var targets []baselineDetailTarget
	add := func(target baselineDetailTarget) {
		if target.id == "" || containsBaselineTarget(targets, target) {
			return
		}
		targets = append(targets, target)
	}
	for index, protection := range w.sortedBaselineProtections(w.catalog.ProtectionsForAsset(asset.ID)) {
		add(baselineProtectionTarget("asset:"+asset.ID, index, protection))
	}
	if asset.Kind == "object" {
		for _, field := range w.controlledBaselineFields(asset.ID) {
			add(baselineDetailTarget{kind: baselineTargetAsset, id: field.ID, rowID: "child:" + field.ID})
			for index, protection := range w.sortedBaselineProtections(w.catalog.ProtectionsForAsset(field.ID)) {
				add(baselineProtectionTarget("child:"+field.ID, index, protection))
			}
		}
	} else if asset.Kind == "field" && asset.ParentID != "" {
		for index, protection := range w.sortedBaselineProtections(w.catalog.ProtectionsForAsset(asset.ParentID)) {
			add(baselineProtectionTarget("parent:"+asset.ParentID, index, protection))
		}
	}
	return targets
}

func baselineProtectionTarget(
	scope string,
	index int,
	protection BaselineProtection,
) baselineDetailTarget {
	identity := protection.ID
	if identity == "" {
		identity = protection.ControlID
	}
	return baselineDetailTarget{
		kind:  baselineTargetControl,
		id:    protection.ControlID,
		rowID: scope + ":" + strconv.Itoa(index) + ":" + identity,
	}
}

func (w *BaselineWorkspace) controlledBaselineFields(parentID string) []BaselineAsset {
	fields := w.catalog.FieldsForParent(parentID)
	if w.includeUncontrolledAssets {
		return fields
	}
	controlled := make([]BaselineAsset, 0, len(fields))
	for _, field := range fields {
		if len(w.catalog.ProtectionsForAsset(field.ID)) > 0 {
			controlled = append(controlled, field)
		}
	}
	return controlled
}

func (w *BaselineWorkspace) sortedBaselineProtections(
	protections []BaselineProtection,
) []BaselineProtection {
	ordered := append([]BaselineProtection(nil), protections...)
	presenceRank := map[string]int{"present": 0, "partial": 1, "absent": 2}
	sort.SliceStable(ordered, func(left, right int) bool {
		leftRank, ok := presenceRank[ordered[left].Presence]
		if !ok {
			leftRank = 3
		}
		rightRank, ok := presenceRank[ordered[right].Presence]
		if !ok {
			rightRank = 3
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftControl, _ := w.catalog.Control(ordered[left].ControlID)
		rightControl, _ := w.catalog.Control(ordered[right].ControlID)
		leftName := strings.ToLower(leftControl.Name)
		rightName := strings.ToLower(rightControl.Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return ordered[left].ControlID < ordered[right].ControlID
	})
	return ordered
}

func containsBaselineTarget(targets []baselineDetailTarget, target baselineDetailTarget) bool {
	for _, candidate := range targets {
		if candidate == target {
			return true
		}
	}
	return false
}

func adjacentBaselineTarget(
	targets []baselineDetailTarget,
	current baselineDetailTarget,
	direction int,
) baselineDetailTarget {
	if len(targets) == 0 {
		return baselineDetailTarget{}
	}
	index := 0
	for candidateIndex, candidate := range targets {
		if candidate == current {
			index = candidateIndex
			break
		}
	}
	return targets[clampBaseline(index+direction, 0, len(targets)-1)]
}

func adjacentBaselineControlTarget(
	targets []baselineDetailTarget,
	current baselineDetailTarget,
	direction int,
) baselineDetailTarget {
	controls := make([]baselineDetailTarget, 0, len(targets))
	for _, target := range targets {
		if target.kind == baselineTargetControl {
			controls = append(controls, target)
		}
	}
	if len(controls) == 0 {
		return baselineDetailTarget{}
	}
	index := 0
	for candidateIndex, candidate := range controls {
		if candidate == current {
			index = candidateIndex
			break
		}
	}
	return controls[clampBaseline(index+direction, 0, len(controls)-1)]
}

type baselinePanelRow struct {
	text   string
	tone   string
	target baselineDetailTarget
}

type baselineStyle struct {
	enabled bool
}

func (s baselineStyle) apply(code, value string) string {
	if !s.enabled {
		return value
	}
	return "\033[" + code + "m" + value + "\033[0m"
}

func (s baselineStyle) bold(value string) string      { return s.apply("1", value) }
func (s baselineStyle) dim(value string) string       { return s.apply("2", value) }
func (s baselineStyle) cyan(value string) string      { return s.apply("1;36", value) }
func (s baselineStyle) green(value string) string     { return s.apply("1;32", value) }
func (s baselineStyle) yellow(value string) string    { return s.apply("1;33", value) }
func (s baselineStyle) highlight(value string) string { return s.apply("1;30;46", value) }
func (s baselineStyle) origin(value string) string    { return s.apply("37;100", value) }
func (s baselineStyle) code(value string) string {
	return s.apply("38;2;205;230;216;48;2;24;48;38", value)
}

func baselineColorEnabled(file *os.File) bool {
	if file == nil || !term.IsTerminal(int(file.Fd())) {
		return false
	}
	_, disabled := os.LookupEnv("NO_COLOR")
	return !disabled
}

func (w *BaselineWorkspace) renderBaselineFrame(
	state baselineWorkspaceState,
	width, height int,
) string {
	state, _ = w.normalizeBaselineState(state)
	width = max(baselineMinWidth, width)
	height = max(baselineMinHeight, height)
	style := baselineStyle{enabled: w.color}

	normalLeftWidth := max(32, min(44, width/3))
	thirdOpen := false
	if state.openedControlID != "" {
		_, thirdOpen = w.catalog.Control(state.openedControlID)
	}
	leftWidth := normalLeftWidth
	rightWidth := width - leftWidth - baselineGap
	controlWidth := 0
	if thirdOpen {
		leftWidth = max(18, normalLeftWidth/2)
		detailSpace := width - leftWidth - baselineGap*2
		rightWidth = detailSpace / 2
		controlWidth = detailSpace - rightWidth
	}

	header := w.repositoryHeader(style, width, true)
	if len(header) > height-11 {
		header = w.repositoryHeader(style, width, false)
	}
	header = append(header, "")
	panelHeight := max(8, height-len(header)-2)

	kind := baselineAssetKinds[state.kindIndex]
	assets, _ := w.filteredBaselineAssets(kind, state.activeQuery())
	leftRows := w.baselineAssetListRows(
		state,
		kind,
		assets,
		w.baselineAssetCounts(),
		panelHeight-2,
		leftWidth-4,
	)
	left := w.baselinePanel("Assets", leftRows, leftWidth, panelHeight, style)

	var detailRows []baselinePanelRow
	if state.focus != baselineFocusTypes && len(assets) > 0 {
		detailRows = w.baselineAssetDetailRows(
			assets[state.assetIndex],
			rightWidth-4,
			state.focus == baselineFocusDetail || state.focus == baselineFocusControl,
			state.detailTarget,
		)
		detailHeight := panelHeight - 2
		maxScroll := max(0, len(detailRows)-detailHeight)
		detailScroll := min(state.detailScroll, maxScroll)
		if state.focus == baselineFocusDetail || state.focus == baselineFocusControl {
			selectedRow := baselineSelectedTargetRow(detailRows, state.detailTarget)
			if selectedRow >= 0 {
				if selectedRow < detailScroll {
					detailScroll = selectedRow
				} else if selectedRow >= detailScroll+detailHeight {
					detailScroll = selectedRow - detailHeight + 1
				}
			}
		}
		detailRows = detailRows[detailScroll:]
	} else if state.focus != baselineFocusTypes {
		detailRows = []baselinePanelRow{
			{text: "NO MATCHING ASSETS", tone: "label"},
			{},
			{text: "Try another asset type or clear the filter.", tone: "dim"},
		}
	}
	right := w.baselinePanel("Asset details", detailRows, rightWidth, panelHeight, style)

	body := make([]string, 0, panelHeight)
	if thirdOpen {
		controlRows := w.baselineControlRows(state.openedControlID, controlWidth-4)
		controlHeight := panelHeight - 2
		controlScroll := min(state.controlDetailScroll, max(0, len(controlRows)-controlHeight))
		controlRows = controlRows[controlScroll:]
		control := w.baselinePanel("Control", controlRows, controlWidth, panelHeight, style)
		for index := range left {
			body = append(body, left[index]+strings.Repeat(" ", baselineGap)+right[index]+
				strings.Repeat(" ", baselineGap)+control[index])
		}
	} else {
		for index := range left {
			body = append(body, left[index]+strings.Repeat(" ", baselineGap)+right[index])
		}
	}

	footer := ""
	if state.filtering {
		footer = "Filter assets: " + sanitizeBaselineText(state.draftQuery, false) + "█   Enter apply   Esc cancel"
	} else {
		clear := ""
		if state.query != "" {
			clear = "  C clear"
		}
		switch state.focus {
		case baselineFocusDetail:
			footer = "↑↓ detail item  Enter/→ preview  ←/Esc asset list  Q exit"
		case baselineFocusControl:
			footer = "↑↓ switch control  Pg↑↓ scroll  ←/Esc asset details  Q exit"
		case baselineFocusTypes:
			footer = "↑↓ asset type  Enter/→ asset list  ←/Esc runs  Q exit"
		default:
			footer = "↑↓ asset  Enter/→ details  ←/Esc types  / find  Pg↑↓ detail" + clear + "  Q exit"
		}
	}

	lines := append(append(header, body...), "", baselineFit(footer, width))
	return strings.Join(lines, "\r\n")
}

func (w *BaselineWorkspace) baselineAssetListRows(
	state baselineWorkspaceState,
	kind string,
	assets []BaselineDiscovery,
	counts map[string]int,
	visibleRows, contentWidth int,
) []baselinePanelRow {
	query := state.activeQuery()
	resultLabel := strings.ToUpper(baselineAssetKindLabels[kind]) + "  " + strconv.Itoa(len(assets))
	if query != "" {
		resultLabel += "  ·  \"" + sanitizeBaselineText(query, false) + "\""
	}
	countWidth := visibleLen("ASSETS")
	typeWidth := max(8, contentWidth-2-2-countWidth)
	rows := []baselinePanelRow{
		{text: "ASSET TYPES", tone: "label"},
		{text: "  " + baselinePadRight("TYPE", typeWidth) + "  " + baselinePadLeft("ASSETS", countWidth), tone: "dim"},
	}
	for _, candidate := range baselineAssetKinds {
		current := candidate == kind
		focused := state.focus == baselineFocusTypes && current
		marker := " "
		tone := ""
		if focused {
			marker = "›"
			tone = "selected"
		} else if current {
			tone = "origin"
		}
		rows = append(rows, baselinePanelRow{
			text: marker + " " + baselinePadRightMinimum(baselineAssetKindLabels[candidate], typeWidth) +
				"  " + baselinePadLeft(strconv.Itoa(counts[candidate]), countWidth),
			tone: tone,
		})
	}

	controlCountWidth := visibleLen("CONTROLS")
	assetNameWidth := max(8, contentWidth-2-2-controlCountWidth)
	rows = append(rows,
		baselinePanelRow{},
		baselinePanelRow{text: resultLabel, tone: "label"},
		baselinePanelRow{
			text: "  " + baselineFit("ASSET", assetNameWidth) + "  CONTROLS",
			tone: "dim",
		},
	)
	listHeight := max(1, visibleRows-len(rows)-1)
	start := max(0, min(state.assetIndex-listHeight/2, len(assets)-listHeight))
	end := min(len(assets), start+listHeight)
	for index := start; index < end; index++ {
		asset := assets[index]
		current := index == state.assetIndex
		selected := state.focus == baselineFocusAssets && current
		origin := (state.focus == baselineFocusDetail || state.focus == baselineFocusControl) && current
		marker := " "
		tone := ""
		if selected || origin {
			marker = "›"
		}
		if selected {
			tone = "selected"
		} else if origin {
			tone = "origin"
		}
		rows = append(rows, baselinePanelRow{
			text: marker + " " + baselineFit(
				sanitizeBaselineText(w.catalog.AssetDisplayName(asset.ID), false),
				assetNameWidth,
			) + "  " + baselinePadLeft(strconv.Itoa(w.catalog.ReviewControlCount(asset.ID)), controlCountWidth),
			tone: tone,
		})
	}
	if len(assets) > listHeight {
		rows = append(rows, baselinePanelRow{
			text: fmt.Sprintf("%d–%d of %d", start+1, end, len(assets)),
			tone: "dim",
		})
	}
	return rows
}

func (w *BaselineWorkspace) baselineAssetDetailRows(
	asset BaselineDiscovery,
	contentWidth int,
	detailFocus bool,
	selectedTarget baselineDetailTarget,
) []baselinePanelRow {
	displayName := sanitizeBaselineText(w.catalog.AssetDisplayName(asset.ID), false)
	kindLabel := strings.ToUpper(asset.Kind)
	if asset.Kind == "endpoint" {
		kindLabel = "ENDPOINT GROUP"
	}
	nameWidth := max(8, contentWidth-visibleLen(kindLabel)-2)
	rows := []baselinePanelRow{{
		text: baselineFit(displayName, nameWidth) + "  " + kindLabel,
		tone: "title",
	}}
	if asset.Location != "" {
		rows = append(rows, baselineWrappedRows(asset.Location, contentWidth, "dim", "")...)
	}
	if asset.Description != "" {
		rows = append(rows, baselinePanelRow{}, baselinePanelRow{text: "WHAT IT IS", tone: "label"})
		rows = append(rows, baselineWrappedRows(asset.Description, contentWidth, "", "")...)
	}

	controlHeading := "ASSOCIATED CONTROLS"
	if asset.Kind == "object" {
		controlHeading = "CONTROLS ON THIS OBJECT"
	} else if asset.Kind == "field" {
		controlHeading = "CONTROLS ON THIS FIELD"
	}
	directProtections := w.catalog.ProtectionsForAsset(asset.ID)
	rows = append(rows,
		baselinePanelRow{},
		baselinePanelRow{text: controlHeading + "  " + strconv.Itoa(asset.ControlCount), tone: "label"},
	)
	if len(directProtections) == 0 {
		empty := "No controls are associated with this asset."
		if asset.Kind == "object" {
			empty = "No object-level controls detected."
		} else if asset.Kind == "field" {
			empty = "No field-specific controls detected."
		}
		rows = append(rows, baselinePanelRow{text: empty, tone: "dim"})
	}
	rows = w.appendBaselineControlTable(
		rows,
		directProtections,
		contentWidth,
		detailFocus,
		selectedTarget,
		0,
		"asset:"+asset.ID,
	)

	if asset.Kind == "endpoint" && len(asset.Routes) > 0 {
		routes := w.catalog.EndpointDisplayRoutes(asset.ID)
		rows = append(rows,
			baselinePanelRow{},
			baselinePanelRow{text: "ENDPOINTS  " + strconv.Itoa(len(routes)), tone: "label"},
		)
		rows = append(rows, baselineWrappedRows(
			"Inventory for this group; controls above are not attributed to individual endpoints.",
			contentWidth,
			"dim",
			"",
		)...)
		for _, route := range routes {
			duplicate := ""
			if len(route.Definitions) > 1 {
				duplicate = fmt.Sprintf(" · %d definitions", len(route.Definitions))
			}
			method := strings.ToUpper(sanitizeBaselineText(route.Method, false))
			path := sanitizeBaselineText(route.Path, false)
			rows = append(rows, baselineRouteRows(method, path+duplicate, contentWidth)...)
		}
	}

	if asset.Kind == "object" {
		fields := w.controlledBaselineFields(asset.ID)
		childAssociations := 0
		for _, field := range fields {
			childAssociations += len(w.catalog.ProtectionsForAsset(field.ID))
		}
		controlNoun := "CONTROLS"
		if childAssociations == 1 {
			controlNoun = "CONTROL"
		}
		fieldNoun := "FIELDS"
		if len(fields) == 1 {
			fieldNoun = "FIELD"
		}
		rows = append(rows,
			baselinePanelRow{},
			baselinePanelRow{
				text: fmt.Sprintf("%s ON CHILD %s  %d", controlNoun, fieldNoun, childAssociations),
				tone: "label",
			},
		)
		if len(fields) == 0 {
			rows = append(rows, baselinePanelRow{text: "No controls on child fields detected.", tone: "dim"})
		}
		for _, field := range fields {
			protections := w.catalog.ProtectionsForAsset(field.ID)
			target := baselineDetailTarget{kind: baselineTargetAsset, id: field.ID, rowID: "child:" + field.ID}
			selected := detailFocus && selectedTarget == target
			marker := " "
			tone := "strong"
			if selected {
				marker = "›"
				tone = "selected"
			}
			fieldName := sanitizeBaselineText(w.catalog.AssetDisplayName(field.ID), false)
			if _, suffix, found := strings.Cut(fieldName, " › "); found {
				fieldName = suffix
			}
			controlCount := uniqueBaselineProtectionControls(protections)
			rows = append(rows, baselinePanelRow{
				text:   fmt.Sprintf("%s %s · %d control%s", marker, fieldName, controlCount, baselinePluralSuffix(controlCount)),
				tone:   tone,
				target: target,
			})
			rows = w.appendBaselineControlTable(
				rows,
				protections,
				contentWidth,
				detailFocus,
				selectedTarget,
				2,
				"child:"+field.ID,
			)
		}
		if len(fields) > 0 {
			rows = append(rows, baselinePanelRow{text: "Enter to select a field; Enter again to open.", tone: "dim"})
		}
	}

	if asset.Kind == "field" && asset.ParentID != "" {
		if _, ok := w.catalog.Asset(asset.ParentID); ok {
			parentProtections := w.catalog.ProtectionsForAsset(asset.ParentID)
			noun := "CONTROLS"
			if len(parentProtections) == 1 {
				noun = "CONTROL"
			}
			rows = append(rows,
				baselinePanelRow{},
				baselinePanelRow{
					text: fmt.Sprintf("%s ON PARENT OBJECT  %d", noun, len(parentProtections)),
					tone: "label",
				},
			)
			if len(parentProtections) == 0 {
				rows = append(rows, baselinePanelRow{text: "No object-level controls detected.", tone: "dim"})
			}
			rows = w.appendBaselineControlTable(
				rows,
				parentProtections,
				contentWidth,
				detailFocus,
				selectedTarget,
				0,
				"parent:"+asset.ParentID,
			)
		}
	}
	return rows
}

func (w *BaselineWorkspace) appendBaselineControlTable(
	rows []baselinePanelRow,
	protections []BaselineProtection,
	contentWidth int,
	detailFocus bool,
	selectedTarget baselineDetailTarget,
	indent int,
	scope string,
) []baselinePanelRow {
	protections = w.sortedBaselineProtections(protections)
	if len(protections) == 0 {
		return rows
	}
	available := max(24, contentWidth-indent)
	wide := available >= 58
	propertyWidth := min(15, max(12, available/3))
	if wide {
		propertyWidth = 17
	}
	controlWidth := 0
	header := ""
	if wide {
		stateWidth := 9
		controlWidth = max(12, available-2-2-propertyWidth-2-stateWidth)
		header = strings.Repeat(" ", indent) + "  " + baselineFit("CONTROL", controlWidth) +
			"  " + baselineFit("PROPERTY", propertyWidth) + "  STATE"
	} else {
		controlWidth = max(10, available-4-2-propertyWidth)
		header = strings.Repeat(" ", indent) + "    " + baselineFit("CONTROL", controlWidth) + "  PROPERTY"
	}
	rows = append(rows, baselinePanelRow{text: header, tone: "dim"})

	for index, protection := range protections {
		control, ok := w.catalog.Control(protection.ControlID)
		if !ok {
			continue
		}
		target := baselineProtectionTarget(scope, index, protection)
		selected := detailFocus && selectedTarget == target
		pointer := " "
		if selected {
			pointer = "›"
		}
		marker := baselinePresenceGlyph(protection.Presence)
		property := baselineControlCategory(control.Property)
		controlName := baselineHumanName(control.Name)
		text := ""
		if wide {
			text = strings.Repeat(" ", indent) + pointer + " " +
				baselineFit(sanitizeBaselineText(controlName, false), controlWidth) + "  " +
				baselineFit(sanitizeBaselineText(property, false), propertyWidth) + "  " +
				baselinePresenceLabel(protection.Presence)
		} else {
			text = strings.Repeat(" ", indent) + pointer + " " + marker + " " +
				baselineFit(sanitizeBaselineText(controlName, false), controlWidth) + "  " +
				baselineFit(sanitizeBaselineText(property, false), propertyWidth)
		}
		tone := "control-" + protection.Presence
		if selected {
			tone = "selected"
		}
		rows = append(rows, baselinePanelRow{text: text, tone: tone, target: target})
	}
	return rows
}

func (w *BaselineWorkspace) baselineControlRows(
	controlID string,
	contentWidth int,
) []baselinePanelRow {
	control, ok := w.catalog.Control(controlID)
	if !ok {
		return []baselinePanelRow{{text: "CONTROL NOT FOUND", tone: "label"}}
	}
	property := strings.ToUpper(baselineControlCategory(control.Property))
	titleWidth := max(8, contentWidth-visibleLen(property)-2)
	rows := []baselinePanelRow{{
		text: baselineFit(sanitizeBaselineText(baselineHumanName(control.Name), false), titleWidth) + "  " + property,
		tone: "title",
	}}
	rows = append(rows, baselinePanelRow{}, baselinePanelRow{text: "SECURITY GUIDANCE", tone: "label"})
	rows = append(rows, baselineWrappedRows(control.Description, contentWidth, "", "")...)

	applications := make([]BaselineControlApplication, 0)
	for _, application := range w.catalog.ControlApplications(controlID) {
		if baselineKindIndex(application.Kind) >= 0 {
			applications = append(applications, application)
		}
	}
	assetNoun := "ASSETS"
	if len(applications) == 1 {
		assetNoun = "ASSET"
	}
	rows = append(rows,
		baselinePanelRow{},
		baselinePanelRow{text: fmt.Sprintf("APPLIES TO  %d %s", len(applications), assetNoun), tone: "label"},
	)
	if len(applications) > 0 {
		wide := contentWidth >= 44
		typeWidth := 8
		if wide {
			typeWidth = 10
			stateWidth := 9
			assetWidth := max(12, contentWidth-typeWidth-stateWidth-4)
			rows = append(rows, baselinePanelRow{
				text: baselineFit("TYPE", typeWidth) + "  " + baselineFit("ASSET", assetWidth) + "  STATE",
				tone: "dim",
			})
			for _, application := range applications {
				rows = append(rows, baselinePanelRow{
					text: baselineFit(strings.ToUpper(sanitizeBaselineText(application.Kind, false)), typeWidth) + "  " +
						baselineFit(sanitizeBaselineText(application.Name, false), assetWidth) + "  " +
						baselinePresenceLabel(application.Presence),
					tone: "control-" + application.Presence,
				})
			}
		} else {
			assetWidth := max(10, contentWidth-typeWidth-5)
			rows = append(rows, baselinePanelRow{
				text: "  " + baselineFit("TYPE", typeWidth) + "  ASSET",
				tone: "dim",
			})
			for _, application := range applications {
				rows = append(rows, baselinePanelRow{
					text: baselinePresenceGlyph(application.Presence) + " " +
						baselineFit(strings.ToUpper(sanitizeBaselineText(application.Kind, false)), typeWidth) + "  " +
						baselineFit(sanitizeBaselineText(application.Name, false), assetWidth),
					tone: "control-" + application.Presence,
				})
			}
		}
	} else {
		rows = append(rows, baselinePanelRow{text: "No endpoint, object, field, or code Assets linked.", tone: "dim"})
	}

	rows = append(rows, baselinePanelRow{}, baselinePanelRow{text: "IMPLEMENTATION FORMS", tone: "label"})
	forms := w.catalog.ControlForms(controlID)
	if len(forms) > 0 {
		formWidth := max(10, (contentWidth-4)/2)
		locationWidth := max(10, contentWidth-formWidth-4)
		rows = append(rows, baselinePanelRow{
			text: "  " + baselineFit("FORM", formWidth) + "  LOCATION",
			tone: "dim",
		})
		for index, form := range forms {
			var anchor *BaselineAnchor
			for anchorIndex := range form.Anchors {
				if form.Anchors[anchorIndex].Quote != "" {
					anchor = &form.Anchors[anchorIndex]
					break
				}
			}
			if anchor == nil && len(form.Anchors) > 0 {
				anchor = &form.Anchors[0]
			}
			location := "No code location"
			if anchor != nil && anchor.Decl != "" {
				location = anchor.Decl
			}
			rows = append(rows, baselinePanelRow{
				text: "  " + baselineFit(sanitizeBaselineText(baselineHumanName(form.Name), false), formWidth) + "  " +
					baselineFit(sanitizeBaselineText(location, false), locationWidth),
			})
			if anchor != nil && anchor.Quote != "" {
				rows = append(rows, baselineCodeRows(anchor.Quote, contentWidth)...)
			} else {
				rows = append(rows, baselinePanelRow{text: "  No quoted code evidence.", tone: "dim"})
			}
			if index < len(forms)-1 {
				rows = append(rows, baselinePanelRow{})
			}
		}
	}
	return rows
}

func (w *BaselineWorkspace) baselinePanel(
	title string,
	rows []baselinePanelRow,
	width, height int,
	style baselineStyle,
) []string {
	inner := width - 2
	topLabel := " " + title + " "
	top := "┌" + topLabel + strings.Repeat("─", max(0, inner-visibleLen(topLabel))) + "┐"
	result := []string{style.dim(top)}
	for index := 0; index < height-2; index++ {
		row := baselinePanelRow{}
		if index < len(rows) {
			row = rows[index]
		}
		content := baselineFit(" "+row.text, inner)
		result = append(result, style.dim("│")+baselineTone(style, content, row.tone)+style.dim("│"))
	}
	result = append(result, style.dim("└"+strings.Repeat("─", inner)+"┘"))
	return result
}

func baselineTone(style baselineStyle, value, tone string) string {
	switch tone {
	case "selected":
		return style.highlight(value)
	case "origin":
		return style.origin(value)
	case "label":
		return style.cyan(value)
	case "title", "strong":
		return style.bold(value)
	case "dim":
		return style.dim(value)
	case "code":
		return style.code(value)
	}
	if strings.HasPrefix(tone, "control-") {
		presence := strings.TrimPrefix(tone, "control-")
		wideBadge := baselinePresenceLabel(presence)
		narrowBadge := baselinePresenceGlyph(presence)
		badge := narrowBadge
		if strings.Contains(value, wideBadge) {
			badge = wideBadge
		}
		apply := func(item string) string { return item }
		switch presence {
		case "present":
			apply = style.green
		case "partial":
			apply = style.yellow
		case "absent":
			apply = style.dim
		}
		if badge != "" && strings.Contains(value, badge) {
			return strings.Replace(value, badge, apply(badge), 1)
		}
	}
	return value
}

func baselineSelectedTargetRow(rows []baselinePanelRow, target baselineDetailTarget) int {
	for index, row := range rows {
		if row.target == target {
			return index
		}
	}
	return -1
}

func (w *BaselineWorkspace) repositoryHeader(
	style baselineStyle,
	width int,
	detailed bool,
) []string {
	width = max(1, width)
	repo := baselineMap(w.blueprint["repo"])
	name := baselineString(repo["name"])
	if name == "" {
		name = filepath.Base(w.repositoryID)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = w.catalog.Repo
		}
	}
	name = sanitizeBaselineText(name, false)
	identity := "KONVU  " + name
	status := "BASELINE READY"
	gap := width - visibleLen(identity) - visibleLen(status)
	first := baselineFit(identity, width)
	if gap >= 2 {
		first = style.bold(identity) + strings.Repeat(" ", gap) + style.green(status)
	} else {
		first = style.bold(first)
	}

	rows := []string{first}
	snapshot := w.baselineSnapshotLine(repo)
	if snapshot != "" {
		rows = append(rows, style.dim(baselineTruncate(snapshot, width, true)))
	}
	rows = append(rows, "")
	summary := baselineString(repo["summary"])
	if summary == "" {
		summary = "See which security Controls apply to the repository's endpoint, object, field, and code Assets."
	}
	for _, row := range baselineWrapText(summary, width, "") {
		rows = append(rows, style.dim(row))
	}
	rows = append(rows, "")

	metadata := w.baselineMetadataRows(detailed)
	if len(metadata) > 0 {
		for _, row := range metadata {
			label, value, found := strings.Cut(row, "\t")
			if !found {
				rows = append(rows, baselineFit(row, width))
				continue
			}
			prefix := baselinePadRight(label, 12)
			wrapped := baselineWrapText(value, max(12, width-12), "")
			if len(wrapped) == 0 {
				wrapped = []string{""}
			}
			rows = append(rows, style.cyan(prefix)+wrapped[0])
			for _, continuation := range wrapped[1:] {
				rows = append(rows, strings.Repeat(" ", 12)+continuation)
			}
		}
	}
	return rows
}

func (w *BaselineWorkspace) baselineSnapshotLine(repo map[string]any) string {
	parts := make([]string, 0, 4)
	if w.commit != "" {
		parts = append(parts, "commit "+baselineShortCommit(w.commit))
	}
	if layout := baselineString(repo["layout"]); layout != "" {
		parts = append(parts, strings.ToLower(layout))
	}
	if components := baselineSlice(w.blueprint["components"]); len(components) > 0 {
		parts = append(parts, baselineCountLabel(len(components), "component"))
	}
	metrics := baselineMap(w.blueprint["metrics"])
	if lines, ok := baselineNumber(metrics["source_lines"]); ok && lines > 0 {
		parts = append(parts, baselineCompactNumber(lines)+" LOC")
	}
	return strings.Join(parts, " · ")
}

func (w *BaselineWorkspace) baselineMetadataRows(detailed bool) []string {
	rows := make([]string, 0, 4)
	if detailed {
		if components := baselineComponentSummary(baselineSlice(w.blueprint["components"])); components != "" {
			rows = append(rows, "COMPONENTS\t"+components)
		}
		if stack := baselineStackSummary(w.blueprint); stack != "" {
			rows = append(rows, "STACK\t"+stack)
		}
		if data := baselineNamedSummary(baselineSlice(w.blueprint["databases"])); data != "" {
			rows = append(rows, "DATA\t"+data)
		}
	}

	counts := w.baselineAssetCounts()
	routeCount := 0
	endpoints, _ := w.baselineAssets("endpoint")
	for _, endpoint := range endpoints {
		routeCount += len(w.catalog.EndpointDisplayRoutes(endpoint.ID))
	}
	baselineParts := []string{
		baselineCountLabel(w.catalog.ControlCount(), "control"),
		fmt.Sprintf(
			"%s (%s)",
			baselineCountLabel(counts["endpoint"], "endpoint group"),
			baselineCountLabel(routeCount, "route"),
		),
		baselineCountLabel(counts["object"], "object"),
		baselineCountLabel(counts["field"], "field"),
	}
	if counts["code"] > 0 {
		baselineParts = append(baselineParts, baselineCountLabel(counts["code"], "code Asset"))
	}
	rows = append(rows, "BASELINE\t"+strings.Join(baselineParts, " · "))
	return rows
}

func baselineComponentSummary(components []any) string {
	if len(components) == 0 {
		return ""
	}
	counts := make(map[string]int)
	libraryNames := make([]string, 0)
	infrastructureNames := make([]string, 0)
	for _, value := range components {
		component := baselineMap(value)
		kind := strings.ToLower(baselineString(component["kind"]))
		if kind == "" {
			kind = strings.ToLower(baselineString(component["role"]))
		}
		name := baselineString(component["name"])
		if name == "" {
			name = baselineString(component["id"])
		}
		switch kind {
		case "frontend":
			counts["frontend"]++
		case "worker":
			counts["worker"]++
		case "service":
			counts["service"]++
		case "library":
			counts["library"]++
			libraryNames = append(libraryNames, name)
		case "infrastructure":
			counts["infrastructure"]++
			infrastructureNames = append(infrastructureNames, name)
		}
	}
	parts := make([]string, 0, 6)
	for _, kind := range []string{"service", "worker"} {
		if count := counts[kind]; count > 0 {
			parts = append(parts, baselineCountLabel(count, kind))
		}
	}
	if count := counts["frontend"]; count > 0 {
		if count == 1 {
			parts = append(parts, "frontend")
		} else {
			parts = append(parts, baselineCountLabel(count, "frontend"))
		}
	}
	if count := counts["library"]; count > 0 {
		if strings.Contains(strings.ToLower(strings.Join(libraryNames, " ")), "model") {
			parts = append(parts, "shared models")
		} else {
			parts = append(parts, baselineCountLabel(count, "library"))
		}
	}
	if count := counts["infrastructure"]; count > 0 {
		if strings.Contains(strings.ToLower(strings.Join(infrastructureNames, " ")), "kubernetes") {
			parts = append(parts, "Kubernetes")
		} else {
			parts = append(parts, "infrastructure")
		}
	}
	return strings.Join(parts, " · ")
}

func baselineStackSummary(blueprint map[string]any) string {
	languages := append([]any(nil), baselineSlice(blueprint["languages"])...)
	sort.SliceStable(languages, func(left, right int) bool {
		leftLines, _ := baselineNumber(baselineMap(languages[left])["lines"])
		rightLines, _ := baselineNumber(baselineMap(languages[right])["lines"])
		return leftLines > rightLines
	})
	if len(languages) > 2 {
		languages = languages[:2]
	}

	records := make([]any, 0, len(languages)+len(baselineSlice(blueprint["frameworks"]))+len(baselineSlice(blueprint["orms"])))
	records = append(records, languages...)
	records = append(records, baselineSlice(blueprint["frameworks"])...)
	records = append(records, baselineSlice(blueprint["orms"])...)

	names := baselineNamedValues(records)
	for index, name := range names {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "react and vite", "react + vite":
			names[index] = "React/Vite"
		}
	}
	return strings.Join(baselineUniqueNames(names), " · ")
}

func baselineNamedSummary(values []any) string {
	return strings.Join(baselineNamedValues(values), " · ")
}

func baselineUniqueNames(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func baselineNamedValues(values []any) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		name := ""
		switch typed := value.(type) {
		case string:
			name = typed
		case map[string]any:
			for _, key := range []string{"name", "language", "framework", "database", "orm", "kind"} {
				if name = baselineString(typed[key]); name != "" {
					break
				}
			}
		}
		name = sanitizeBaselineText(name, false)
		folded := strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, ok := seen[folded]; ok {
			continue
		}
		seen[folded] = struct{}{}
		result = append(result, name)
	}
	return result
}

func baselineMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func baselineSlice(value any) []any {
	if result, ok := value.([]any); ok {
		return result
	}
	return nil
}

func baselineString(value any) string {
	result, _ := value.(string)
	return strings.TrimSpace(sanitizeBaselineText(result, false))
}

func baselineNumber(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case jsonNumber:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

// jsonNumber is kept local so this renderer does not need to depend on the
// concrete decoder configuration used by its caller.
type jsonNumber string

func baselineShortCommit(value string) string {
	value = sanitizeBaselineText(strings.TrimSpace(value), false)
	if visibleLen(value) <= 12 {
		return value
	}
	return baselineTruncate(value, 12, false)
}

func baselineCompactNumber(value int) string {
	thousands := value / 1000
	remainder := value % 1000
	if remainder > 500 || (remainder == 500 && thousands%2 != 0) {
		thousands++
	}
	return baselineGroupedNumber(thousands) + "K"
}

func baselineCountLabel(count int, singular string) string {
	suffix := ""
	if count != 1 {
		suffix = "s"
	}
	digits := baselineGroupedNumber(max(0, count))
	return digits + " " + singular + suffix
}

func baselineGroupedNumber(value int) string {
	digits := strconv.Itoa(max(0, value))
	for separator := len(digits) - 3; separator > 0; separator -= 3 {
		digits = digits[:separator] + "," + digits[separator:]
	}
	return digits
}

func baselineControlCategory(property string) string {
	property = strings.ToLower(strings.TrimSpace(property))
	switch property {
	case "authentication":
		return "Authentication"
	case "authorization":
		return "Authorization"
	case "confidentiality":
		return "Confidentiality"
	case "integrity":
		return "Integrity"
	case "availability":
		return "Availability"
	case "non_repudiation", "non-repudiation", "accounting", "audit":
		return "Accounting"
	case "privacy":
		return "Privacy"
	default:
		return baselineHumanName(property)
	}
}

func baselinePresenceGlyph(presence string) string {
	switch strings.ToLower(presence) {
	case "present":
		return "●"
	case "partial":
		return "◐"
	case "absent":
		return "○"
	default:
		return "·"
	}
}

func baselinePresenceLabel(presence string) string {
	switch strings.ToLower(presence) {
	case "present":
		return "● Present"
	case "partial":
		return "◐ Partial"
	case "absent":
		return "○ Absent"
	default:
		return strings.ToUpper(sanitizeBaselineText(presence, false))
	}
}

func uniqueBaselineProtectionControls(protections []BaselineProtection) int {
	seen := make(map[string]struct{}, len(protections))
	for _, protection := range protections {
		if protection.ControlID != "" {
			seen[protection.ControlID] = struct{}{}
		}
	}
	return len(seen)
}

func baselinePluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func baselineRouteRows(method, route string, width int) []baselinePanelRow {
	lineWidth := max(20, width)
	prefix := method + strings.Repeat(" ", max(0, 9-visibleLen(method))) + " "
	value := strings.TrimRight(prefix+route, " ")
	if strings.TrimSpace(value) == "" {
		return []baselinePanelRow{{text: strings.TrimRight(prefix, " ")}}
	}
	lines := baselinePythonWrap(value, lineWidth, true)
	rows := make([]baselinePanelRow, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, baselinePanelRow{text: line})
	}
	return rows
}

func baselineWrappedRows(
	value string,
	width int,
	tone string,
	prefix string,
) []baselinePanelRow {
	wrapped := baselineWrapText(value, width, prefix)
	rows := make([]baselinePanelRow, 0, len(wrapped))
	for _, line := range wrapped {
		rows = append(rows, baselinePanelRow{text: line, tone: tone})
	}
	return rows
}

func baselineWrapText(value string, width int, prefix string) []string {
	value = sanitizeBaselineText(value, false)
	width = max(1, width)
	prefix = sanitizeBaselineText(prefix, false)
	lineWidth := max(1, width-visibleLen(prefix))
	wrapped := baselinePythonWrap(value, lineWidth, true)
	lines := make([]string, len(wrapped))
	for index, line := range wrapped {
		lines[index] = prefix + line
	}
	return lines
}

func baselinePythonWrap(value string, width int, dropWhitespace bool) []string {
	width = max(1, width)
	chunks := baselineCodeChunks(value)
	lines := make([]string, 0, 1)
	for len(chunks) > 0 {
		if dropWhitespace && len(lines) > 0 && baselineWhitespaceChunk(chunks[0]) {
			chunks = chunks[1:]
			continue
		}

		line := make([]string, 0, 1)
		lineWidth := 0
		for len(chunks) > 0 {
			chunkWidth := visibleLen(chunks[0])
			if lineWidth+chunkWidth > width {
				break
			}
			line = append(line, chunks[0])
			lineWidth += chunkWidth
			chunks = chunks[1:]
		}

		if len(chunks) > 0 && visibleLen(chunks[0]) > width {
			available := width - lineWidth
			if available > 0 {
				part := baselineSplitLongChunk(chunks[0], available)
				if part == "" {
					_, size := utf8.DecodeRuneInString(chunks[0])
					part = chunks[0][:size]
				}
				line = append(line, part)
				chunks[0] = strings.TrimPrefix(chunks[0], part)
				if chunks[0] == "" {
					chunks = chunks[1:]
				}
			}
		}

		droppedTrailingWhitespace := false
		if dropWhitespace && len(line) > 0 && baselineWhitespaceChunk(line[len(line)-1]) {
			line = line[:len(line)-1]
			droppedTrailingWhitespace = true
		}
		if len(line) > 0 {
			lines = append(lines, strings.Join(line, ""))
			continue
		}
		if droppedTrailingWhitespace && len(chunks) > 0 {
			continue
		}
		if len(chunks) > 0 {
			chunks = chunks[1:]
		}
	}
	return lines
}

func baselineWhitespaceChunk(value string) bool {
	return value != "" && strings.TrimSpace(value) == ""
}

func baselineSplitLongChunk(value string, width int) string {
	part := baselineTruncate(value, width, false)
	if visibleLen(value) <= width {
		return part
	}
	if hyphen := strings.LastIndex(part, "-"); hyphen > 0 && strings.Trim(part[:hyphen], "-") != "" {
		return part[:hyphen+1]
	}
	return part
}

func baselineCodeRows(value string, width int) []baselinePanelRow {
	value = sanitizeBaselineCodeText(value)
	value = strings.Trim(value, "\n")
	if value == "" {
		return nil
	}
	lines := baselineDedentCodeLines(strings.Split(value, "\n"))
	contentWidth := max(12, width-4)
	rows := make([]baselinePanelRow, 0, len(lines))
	for _, line := range lines {
		line = baselineExpandTabs(line, 8)
		for _, wrapped := range baselineWrapCodeLine(line, contentWidth) {
			rows = append(rows, baselinePanelRow{text: "  " + wrapped, tone: "code"})
		}
	}
	return rows
}

func baselineWrapCodeLine(value string, width int) []string {
	if value == "" {
		return []string{""}
	}
	return baselinePythonWrap(value, width, false)
}

func baselineCodeChunks(value string) []string {
	runes := []rune(value)
	chunks := make([]string, 0, 1)
	var current strings.Builder
	currentSpace := len(runes) > 0 && unicode.IsSpace(runes[0])
	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunk := current.String()
		if currentSpace {
			chunks = append(chunks, chunk)
		} else {
			chunks = append(chunks, baselineHyphenChunks(chunk)...)
		}
		current.Reset()
	}
	for _, character := range runes {
		space := unicode.IsSpace(character)
		if current.Len() > 0 && space != currentSpace {
			flush()
		}
		currentSpace = space
		current.WriteRune(character)
	}
	flush()
	return chunks
}

func baselineHyphenChunks(value string) []string {
	runes := []rune(value)
	chunks := make([]string, 0, 1)
	start := 0
	for index := 0; index < len(runes); {
		if runes[index] != '-' {
			index++
			continue
		}
		end := index + 1
		for end < len(runes) && runes[end] == '-' {
			end++
		}
		if end-index >= 2 && index > start && end < len(runes) &&
			baselineDashLeftRune(runes[index-1]) && baselineWordRune(runes[end]) {
			chunks = append(chunks, string(runes[start:index]), string(runes[index:end]))
			start = end
			index = end
			continue
		}
		if end-index == 1 && baselineSingleHyphenBreak(runes, index) {
			chunks = append(chunks, string(runes[start:end]))
			start = end
		}
		index = end
	}
	if start < len(runes) {
		chunks = append(chunks, string(runes[start:]))
	}
	if len(chunks) == 0 {
		return []string{value}
	}
	return chunks
}

func baselineSingleHyphenBreak(value []rune, index int) bool {
	left := index >= 2 && baselineHyphenLetter(value[index-1]) && baselineHyphenLetter(value[index-2])
	if !left && index >= 3 {
		left = baselineHyphenLetter(value[index-1]) && value[index-2] == '-' &&
			baselineHyphenLetter(value[index-3])
	}
	right := index+2 < len(value) && baselineHyphenLetter(value[index+1]) &&
		baselineHyphenLetter(value[index+2])
	if !right && index+3 < len(value) {
		right = baselineHyphenLetter(value[index+1]) && value[index+2] == '-' &&
			baselineHyphenLetter(value[index+3])
	}
	return left && right
}

func baselineHyphenLetter(value rune) bool {
	return unicode.IsLetter(value) || value == '_'
}

func baselineWordRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsNumber(value) || value == '_'
}

func baselineDashLeftRune(value rune) bool {
	return baselineWordRune(value) || strings.ContainsRune(`!"'&.,?`, value)
}

func baselineDedentCodeLines(lines []string) []string {
	margin := ""
	set := false
	for index, line := range lines {
		if strings.Trim(line, " \t") == "" {
			lines[index] = ""
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !set {
			margin = indent
			set = true
			continue
		}
		limit := min(len(margin), len(indent))
		common := 0
		for common < limit && margin[common] == indent[common] {
			common++
		}
		margin = margin[:common]
	}
	if margin == "" {
		return lines
	}
	for index, line := range lines {
		if strings.HasPrefix(line, margin) {
			lines[index] = strings.TrimPrefix(line, margin)
		}
	}
	return lines
}

func baselineExpandTabs(value string, size int) string {
	size = max(1, size)
	var expanded strings.Builder
	column := 0
	for _, character := range value {
		if character == '\t' {
			spaces := size - column%size
			expanded.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			continue
		}
		expanded.WriteRune(character)
		column += runeCells(character)
	}
	return expanded.String()
}

func sanitizeBaselineCodeText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	var result strings.Builder
	for _, character := range value {
		switch {
		case character == '\n' || character == '\t':
			result.WriteRune(character)
		case unicode.IsControl(character):
			result.WriteRune('�')
		default:
			result.WriteRune(character)
		}
	}
	return result.String()
}

func sanitizeBaselineText(value string, preserveNewlines bool) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	var result strings.Builder
	for _, r := range value {
		switch {
		case r == '\n' && preserveNewlines:
			result.WriteRune('\n')
		case r == '\n':
			result.WriteRune(' ')
		case r == '\t':
			result.WriteString("    ")
		case unicode.IsControl(r):
			result.WriteRune('�')
		default:
			result.WriteRune(r)
		}
	}
	return result.String()
}

func baselineFit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = baselineTruncate(value, width, true)
	return value + strings.Repeat(" ", max(0, width-visibleLen(value)))
}

func baselinePadRight(value string, width int) string {
	return baselineFit(value, width)
}

func baselinePadRightMinimum(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-visibleLen(value)))
}

func baselinePadLeft(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = baselineTruncate(value, width, true)
	return strings.Repeat(" ", max(0, width-visibleLen(value))) + value
}

func baselineTruncate(value string, width int, ellipsis bool) string {
	if width <= 0 {
		return ""
	}
	if visibleLen(value) <= width {
		return value
	}
	limit := width
	if ellipsis && width > 1 {
		limit--
	}
	var result strings.Builder
	cells := 0
	for _, r := range value {
		runeWidth := runeCells(r)
		if cells+runeWidth > limit {
			break
		}
		result.WriteRune(r)
		cells += runeWidth
	}
	if ellipsis && width > 1 {
		result.WriteRune('…')
	}
	return result.String()
}

func trimLastBaselineRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}

func clampBaseline(value, low, high int) int {
	if high < low {
		return low
	}
	return min(high, max(low, value))
}
