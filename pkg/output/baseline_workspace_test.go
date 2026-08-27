package output

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBaselineWorkspaceStaticSummaryUsesStoredSnapshot(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	summary := workspace.StaticSummary()

	for _, want := range []string{
		"KONVU  service-api",
		"BASELINE READY",
		"commit 0123456789ab · single_component · 4 components · 548K LOC",
		"Synthetic service used for terminal tests.",
		"COMPONENTS",
		"1 service · 1 worker · frontend · shared models",
		"STACK",
		"Go · TypeScript · Chi · React · SQLC",
		"DATA",
		"PostgreSQL",
		"4 controls · 2 endpoint groups (2 routes) · 1 object · 1 field",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("StaticSummary() missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "\x1b[") {
		t.Fatalf("StaticSummary() contains ANSI escapes: %q", summary)
	}
	if !strings.Contains(summary, "548K LOC\n\nSynthetic service used for terminal tests.\n\nCOMPONENTS") {
		t.Fatalf("StaticSummary() lost repository-header spacing:\n%s", summary)
	}
}

func TestBaselineCompactNumberMatchesPrototypeRoundingAndGrouping(t *testing.T) {
	tests := []struct {
		lines int
		want  string
	}{
		{lines: 1, want: "0K"},
		{lines: 500, want: "0K"},
		{lines: 501, want: "1K"},
		{lines: 999, want: "1K"},
		{lines: 1_500, want: "2K"},
		{lines: 2_500, want: "2K"},
		{lines: 1_500_000, want: "1,500K"},
	}
	for _, test := range tests {
		if got := baselineCompactNumber(test.lines); got != test.want {
			t.Errorf("baselineCompactNumber(%d) = %q, want %q", test.lines, got, test.want)
		}
	}
}

func TestBaselineWorkspaceCompactHeaderKeepsBaselineCounts(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	compact := strings.Join(workspace.repositoryHeader(baselineStyle{}, 100, false), "\n")

	if !strings.Contains(compact, "BASELINE    4 controls · 2 endpoint groups (2 routes) · 1 object · 1 field") {
		t.Fatalf("compact header lost baseline counts:\n%s", compact)
	}
	for _, omitted := range []string{"COMPONENTS", "STACK", "DATA"} {
		if strings.Contains(compact, omitted) {
			t.Fatalf("compact header unexpectedly contains %q:\n%s", omitted, compact)
		}
	}
}

func TestBaselineWorkspaceMetadataMatchesPrototypeCopy(t *testing.T) {
	blueprint := syntheticBaselineBlueprint()
	blueprint["components"] = []any{
		map[string]any{"kind": "service", "name": "api"},
		map[string]any{"kind": "service", "name": "admin"},
		map[string]any{"kind": "worker", "name": "jobs"},
		map[string]any{"kind": "frontend", "name": "dashboard"},
		map[string]any{"kind": "library", "name": "shared model types"},
		map[string]any{"kind": "library", "name": "utilities"},
		map[string]any{"kind": "infrastructure", "name": "Kubernetes manifests"},
	}
	blueprint["languages"] = []any{
		map[string]any{"name": "SQL", "lines": float64(10)},
		map[string]any{"name": "TypeScript", "lines": float64(200)},
		map[string]any{"name": "Go", "lines": float64(300)},
	}
	blueprint["frameworks"] = []any{
		map[string]any{"name": "Chi"},
		map[string]any{"name": "React and Vite"},
		map[string]any{"name": "React + Vite"},
	}
	blueprint["orms"] = []any{map[string]any{"name": "SQLC"}}
	blueprint["databases"] = []any{
		map[string]any{"name": "PostgreSQL"},
		map[string]any{"name": "SQLite"},
	}

	workspace, err := NewBaselineWorkspace(syntheticBaselineResult(), blueprint, "stored-service", "")
	if err != nil {
		t.Fatal(err)
	}
	summary := workspace.StaticSummary()
	for _, want := range []string{
		"2 services · 1 worker · frontend · shared models · Kubernetes",
		"Go · TypeScript · Chi · React/Vite · SQLC",
		"PostgreSQL · SQLite",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("StaticSummary() missing %q:\n%s", want, summary)
		}
	}
}

func TestPickBaselineRepositoryIONavigationClampsAndPreservesSelection(t *testing.T) {
	options := []BaselineRepositoryOption{
		{ID: "stored-a", DisplayName: "API", Commit: "aaaaaaaaaaaa1111"},
		{ID: "stored-b", DisplayName: "Worker", Commit: "bbbbbbbbbbbb2222"},
		{ID: "stored-c", DisplayName: "Web"},
	}

	t.Run("down clamps and right opens", func(t *testing.T) {
		var rendered strings.Builder
		index, opened, err := pickBaselineRepositoryIO(
			bufio.NewReader(strings.NewReader("\x1b[B\x1b[B\x1b[B\x1b[C")),
			&rendered,
			options,
			0,
			true,
		)
		if err != nil || !opened || index != 2 {
			t.Fatalf("selection = (%d, %v, %v), want (2, true, nil)", index, opened, err)
		}
		if !strings.Contains(rendered.String(), "\x1b[1;30;46m") {
			t.Fatalf("picker did not use the selected-row style: %q", rendered.String())
		}
		if !strings.Contains(rendered.String(), "aaaaaaaaaaaa") ||
			strings.Contains(rendered.String(), "aaaaaaaaaaa…") {
			t.Fatalf("picker did not render the 12-character commit prefix: %q", rendered.String())
		}
	})

	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "escape", input: "\x1b"},
		{name: "quit", input: "q"},
	} {
		t.Run(test.name+" keeps current index", func(t *testing.T) {
			index, opened, err := pickBaselineRepositoryIO(
				bufio.NewReader(strings.NewReader(test.input)),
				&strings.Builder{},
				options,
				1,
				false,
			)
			if err != nil || opened || index != 1 {
				t.Fatalf("selection = (%d, %v, %v), want (1, false, nil)", index, opened, err)
			}
		})
	}

	t.Run("control-c cancels", func(t *testing.T) {
		index, opened, err := pickBaselineRepositoryIO(
			bufio.NewReader(strings.NewReader("\x03")),
			&strings.Builder{},
			options,
			1,
			false,
		)
		if index != 1 || opened || !errors.Is(err, ErrBaselineCancelled) {
			t.Fatalf("selection = (%d, %v, %v)", index, opened, err)
		}
	})
}

func TestBaselineRepositoryPickerUsesTenRowViewportAndClearsTitle(t *testing.T) {
	options := make([]BaselineRepositoryOption, 12)
	for index := range options {
		options[index] = BaselineRepositoryOption{
			ID:          fmt.Sprintf("stored-%02d", index+1),
			DisplayName: fmt.Sprintf("Repository %02d", index+1),
		}
	}

	frame := renderBaselineRepositoryPicker(options, 11, baselineStyle{})
	for _, want := range []string{"Repository 03", "Repository 12", "3–12 of 12 · use ↑↓ to scroll"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("viewport missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "Repository 01") || strings.Contains(frame, "Repository 02") {
		t.Fatalf("viewport rendered off-screen repositories:\n%s", frame)
	}

	var cleared strings.Builder
	clearBaselineLines(&cleared, 3)
	wantClear := strings.Repeat("\x1b[1A\r\x1b[2K", 3)
	if cleared.String() != wantClear {
		t.Fatalf("clear sequence = %q, want %q", cleared.String(), wantClear)
	}
}

func TestBaselineRepositoryPickerGolden(t *testing.T) {
	frame := renderBaselineRepositoryPicker([]BaselineRepositoryOption{
		{ID: "stored-api", DisplayName: "acme/api", Commit: "a8c4f21000001111"},
		{ID: "stored-worker", DisplayName: "acme/worker", Commit: "b9d5e32000002222"},
		{ID: "stored-web", DisplayName: "acme/web", Commit: "42d91be000003333"},
	}, 1, baselineStyle{enabled: true})
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(frame)))
	const want = "6e3fd2161e2a85922e92126d4231660188597130a3fd124ba77753eca06e993e"
	if got != want {
		t.Fatalf("repository picker digest = %s, want %s\nframe:\n%s", got, want, frame)
	}
}

func TestReadBaselineKeyWaitsForSplitEscapeSequence(t *testing.T) {
	t.Run("split arrow suffix", func(t *testing.T) {
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer readEnd.Close()
		defer writeEnd.Close()

		if _, err := writeEnd.Write([]byte{27}); err != nil {
			t.Fatal(err)
		}
		writeResult := make(chan error, 1)
		go func() {
			time.Sleep(10 * time.Millisecond)
			_, suffixErr := writeEnd.Write([]byte("[A"))
			writeResult <- suffixErr
		}()

		key, err := readBaselineKey(bufio.NewReader(readEnd), func() (bool, error) {
			return baselineWaitForInput(int(readEnd.Fd()), baselineEscapeSequenceWait)
		})
		if err != nil {
			t.Fatal(err)
		}
		if suffixErr := <-writeResult; suffixErr != nil {
			t.Fatal(suffixErr)
		}
		if key.kind != baselineKeyUp {
			t.Fatalf("split key kind = %v, want Up", key.kind)
		}
	})

	t.Run("standalone escape times out", func(t *testing.T) {
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer readEnd.Close()
		defer writeEnd.Close()

		if _, err := writeEnd.Write([]byte{27}); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		key, err := readBaselineKey(bufio.NewReader(readEnd), func() (bool, error) {
			return baselineWaitForInput(int(readEnd.Fd()), baselineEscapeSequenceWait)
		})
		if err != nil {
			t.Fatal(err)
		}
		if key.kind != baselineKeyEscape {
			t.Fatalf("standalone key kind = %v, want Escape", key.kind)
		}
		if elapsed := time.Since(started); elapsed < baselineEscapeSequenceWait/2 {
			t.Fatalf("standalone Escape returned before suffix timeout: %s", elapsed)
		}
	})
}

func TestReadBaselineKeyRequiresCompletePageSequences(t *testing.T) {
	tests := []struct {
		input string
		want  baselineKeyKind
	}{
		{input: "\x1b[5~", want: baselineKeyPageUp},
		{input: "\x1b[6~", want: baselineKeyPageDown},
		{input: "\x1b[5", want: baselineKeyUnknown},
		{input: "\x1b[6", want: baselineKeyUnknown},
		{input: "\x1b[5x", want: baselineKeyUnknown},
		{input: "\x1b[6x", want: baselineKeyUnknown},
	}
	for _, test := range tests {
		key, err := readBaselineKey(bufio.NewReader(strings.NewReader(test.input)))
		if err != nil {
			t.Fatalf("read %q: %v", test.input, err)
		}
		if key.kind != test.want {
			t.Errorf("read %q kind = %v, want %v", test.input, key.kind, test.want)
		}
	}
}

func TestBaselineWorkspaceReducerPreservesLayeredNavigation(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	state := initialBaselineWorkspaceState()

	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyEnter})
	if state.focus != baselineFocusAssets {
		t.Fatalf("Enter from types focus = %v, want assets", state.focus)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyUp})
	if state.focus != baselineFocusTypes || state.assetIndex != 0 {
		t.Fatalf("Up from first asset = %#v, want type focus", state)
	}

	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyEnter})
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyEnter})
	if state.focus != baselineFocusDetail || state.detailTarget.kind != baselineTargetControl || state.detailTarget.id != "ctrl:auth" {
		t.Fatalf("opened detail state = %#v", state)
	}
	assetIndex := state.assetIndex
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyRight})
	if state.focus != baselineFocusControl || state.openedControlID != "ctrl:auth" {
		t.Fatalf("opened control state = %#v", state)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyDown})
	if state.openedControlID != "ctrl:tenant" || state.assetIndex != assetIndex {
		t.Fatalf("control switch changed asset/order: %#v", state)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyLeft})
	if state.focus != baselineFocusDetail || state.openedControlID != "" {
		t.Fatalf("Left from control state = %#v", state)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyEscape})
	if state.focus != baselineFocusAssets {
		t.Fatalf("Escape from detail focus = %v, want assets", state.focus)
	}
}

func TestBaselineWorkspaceReducerPivotsFromObjectToChildField(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	state := baselineWorkspaceState{kindIndex: 1, focus: baselineFocusDetail}
	state, err := workspace.normalizeBaselineState(state)
	if err != nil {
		t.Fatal(err)
	}
	if state.detailTarget.id != "ctrl:tenant" {
		t.Fatalf("initial object target = %#v", state.detailTarget)
	}

	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyDown})
	if state.detailTarget.kind != baselineTargetAsset || state.detailTarget.id != "field:user_email" {
		t.Fatalf("child field target = %#v", state.detailTarget)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyEnter})
	if state.kindIndex != 2 || state.focus != baselineFocusAssets {
		t.Fatalf("field pivot state = %#v", state)
	}
	fields, err := workspace.filteredBaselineAssets("field", "")
	if err != nil {
		t.Fatal(err)
	}
	if fields[state.assetIndex].ID != "field:user_email" {
		t.Fatalf("pivot selected field = %q", fields[state.assetIndex].ID)
	}
}

func TestBaselineWorkspaceReducerFilteringAndOutcomes(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	state := baselineWorkspaceState{focus: baselineFocusAssets}

	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyFilter})
	for _, r := range "user" {
		state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyCharacter, rune: r})
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyEnter})
	if state.filtering || state.query != "user" {
		t.Fatalf("applied filter state = %#v", state)
	}
	assets, err := workspace.filteredBaselineAssets("endpoint", state.activeQuery())
	if err != nil || len(assets) != 1 || assets[0].ID != "ep:users" {
		t.Fatalf("filtered assets = %#v, err = %v", assets, err)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyClear})
	if state.query != "" {
		t.Fatalf("clear left query %q", state.query)
	}

	_, transition, err := workspace.reduceBaselineState(
		initialBaselineWorkspaceState(),
		baselineKey{kind: baselineKeyLeft},
	)
	if err != nil || transition != baselineTransitionBack {
		t.Fatalf("root Left transition = (%v, %v), want back", transition, err)
	}
	_, transition, err = workspace.reduceBaselineState(state, baselineKey{kind: baselineKeyQuit})
	if err != nil || transition != baselineTransitionQuit {
		t.Fatalf("Q transition = (%v, %v), want quit", transition, err)
	}
	_, transition, err = workspace.reduceBaselineState(state, baselineKey{kind: baselineKeyCancel})
	if err != nil || transition != baselineTransitionCancelled {
		t.Fatalf("Ctrl+C transition = (%v, %v), want cancelled", transition, err)
	}
}

func TestBaselineWorkspaceReducerPagesActiveDetailPanel(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	state := baselineWorkspaceState{
		assetIndex:   1,
		focus:        baselineFocusDetail,
		detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:auth"},
		detailScroll: 7,
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyPageUp})
	if state.detailScroll != 2 {
		t.Fatalf("detail Page Up scroll = %d, want 2", state.detailScroll)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyPageDown})
	if state.detailScroll != 7 {
		t.Fatalf("detail Page Down scroll = %d, want 7", state.detailScroll)
	}

	state.focus = baselineFocusControl
	state.openedControlID = "ctrl:auth"
	state.controlDetailScroll = 3
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyPageUp})
	if state.controlDetailScroll != 0 {
		t.Fatalf("control Page Up scroll = %d, want 0", state.controlDetailScroll)
	}
	state = reduceBaselineForTest(t, workspace, state, baselineKey{kind: baselineKeyPageDown})
	if state.controlDetailScroll != 5 {
		t.Fatalf("control Page Down scroll = %d, want 5", state.controlDetailScroll)
	}
}

func TestBaselineWorkspaceFramesUseTwoAndThreePanelDrilldown(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	workspace.color = true

	initial := workspace.renderBaselineFrame(initialBaselineWorkspaceState(), 120, 36)
	if !strings.Contains(initial, "\x1b[1;30;46m") {
		t.Fatal("initial frame is missing selected type styling")
	}
	if strings.Contains(initial, "ENDPOINT GROUP") {
		t.Fatal("initial type-focused frame rendered asset details")
	}

	detail := baselineWorkspaceState{
		assetIndex:   1,
		focus:        baselineFocusDetail,
		detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:auth"},
	}
	detailFrame := workspace.renderBaselineFrame(detail, 120, 50)
	for _, want := range []string{"┌ Assets ", "┌ Asset details ", "CONTROL", "PROPERTY", "STATE", "ENDPOINTS  2"} {
		if !strings.Contains(detailFrame, want) {
			t.Fatalf("two-panel detail frame missing %q", want)
		}
	}

	detail.focus = baselineFocusControl
	detail.openedControlID = "ctrl:auth"
	controlFrame := workspace.renderBaselineFrame(detail, 132, 52)
	for _, want := range []string{
		"┌ Control ",
		"SECURITY GUIDANCE",
		"APPLIES TO  2 ASSETS",
		"IMPLEMENTATION FORMS",
		"Active token",
		"token.Active",
		"\x1b[38;2;205;230;216;48;2;24;48;38m",
	} {
		if !strings.Contains(controlFrame, want) {
			t.Fatalf("three-panel control frame missing %q", want)
		}
	}
	if strings.Contains(controlFrame, "CODE EVIDENCE") || strings.Contains(controlFrame, " | ") {
		t.Fatalf("control frame used removed prototype copy:\n%s", stripAnsi(controlFrame))
	}
}

func TestBaselineWorkspaceFrameGoldens(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	detail := baselineWorkspaceState{
		assetIndex:   1,
		focus:        baselineFocusDetail,
		detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:auth"},
	}
	control := detail
	control.focus = baselineFocusControl
	control.openedControlID = "ctrl:auth"

	tests := []struct {
		name   string
		state  baselineWorkspaceState
		width  int
		height int
		color  bool
		want   string
	}{
		{name: "minimum_no_color_initial", state: initialBaselineWorkspaceState(), width: 76, height: 22, color: false, want: "4cae9d9d05da7a395926b7116be99a167960d476ad04745c3808602767cfac12"},
		{name: "standard_color_initial", state: initialBaselineWorkspaceState(), width: 120, height: 36, color: true, want: "f3da51234fb2beb5ab06ef4d131259d5c99abc1864fcabd2a3d4a73e034c47e8"},
		{name: "short_no_color_detail", state: detail, width: 100, height: 22, color: false, want: "d9f1ba72e5300ff40f73586c1f8d89681ea1af23c8f34528d1e317d3ce683dd0"},
		{name: "minimum_color_detail", state: detail, width: 76, height: 30, color: true, want: "b53fd5748342ab540e88e1aeacf4135489ce8e64595936f8b9dfbc5447ce3abe"},
		{name: "standard_color_control", state: control, width: 132, height: 52, color: true, want: "19390d52c60f5f539e302c0aebdb78de9109d0ae8230d2a82d9e17aebbb2cf31"},
		{name: "standard_no_color_control", state: control, width: 132, height: 52, color: false, want: "be385819361ce1a6a09fc6e3df0001f2d6bbfa4a841b0c32547a9d39f20a2c68"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace.color = test.color
			frame := workspace.renderBaselineFrame(test.state, test.width, test.height)
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(frame)))
			if got != test.want {
				t.Fatalf("frame digest = %s, want %s\nframe:\n%s", got, test.want, frame)
			}
		})
	}
}

func TestBaselineWorkspaceMatchesApprovedPythonFrames(t *testing.T) {
	workspace := mustPythonOracleBaselineWorkspace(t)
	tests := []struct {
		name   string
		state  baselineWorkspaceState
		width  int
		height int
		color  bool
		want   string
	}{
		{name: "initial", state: initialBaselineWorkspaceState(), width: 120, height: 36, want: "467612139a9c8637e7e8303c874c862da4f26edeac7783b04c18f7f8a127bddc"},
		{name: "endpoint", state: baselineWorkspaceState{focus: baselineFocusDetail, detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:auth"}}, width: 120, height: 36, want: "0a3d61bb30ca89884932d3dadf38a0c31babd7205ff98789c6f57bf3179869b4"},
		{name: "object", state: baselineWorkspaceState{kindIndex: 1, focus: baselineFocusDetail, detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:tenant"}}, width: 120, height: 36, want: "b595be403accf9fa32b3b292b24fd0113f821c4d9e8b82d131375d3b2ce7731b"},
		{name: "object_color", state: baselineWorkspaceState{kindIndex: 1, focus: baselineFocusDetail, detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:tenant"}}, width: 120, height: 36, color: true, want: "c7015a82aa84e131079f6ef021f9d34e6d8f9801c459e3ca8766bbdef74041d9"},
		{name: "field", state: baselineWorkspaceState{kindIndex: 2, focus: baselineFocusDetail, detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:minimize"}}, width: 120, height: 36, want: "973a10a0c5b43f1e6d8b8c0ff3c75eac8d6eb855358a71aec13afd5f7779c13c"},
		{name: "field_color", state: baselineWorkspaceState{kindIndex: 2, focus: baselineFocusDetail, detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:minimize"}}, width: 120, height: 36, color: true, want: "c48fa4eee58b45036324f22e54b631ef9b6392d873edb79fea7ad46724250703"},
		{name: "control", state: baselineWorkspaceState{focus: baselineFocusControl, detailTarget: baselineDetailTarget{kind: baselineTargetControl, id: "ctrl:auth"}, openedControlID: "ctrl:auth"}, width: 132, height: 52, color: true, want: "27fda4812472a866bd92bdc4bb41fb16256360c93455600ef8f0a193044ab4fc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace.color = test.color
			frame := workspace.renderBaselineFrame(test.state, test.width, test.height)
			comparisonFrame := frame
			if test.name == "initial" {
				// The handoff adds repository selection above the approved Python
				// workspace, so only the type-root footer intentionally changes.
				lines := strings.Split(comparisonFrame, "\r\n")
				lines[len(lines)-1] = baselineFit("↑↓ asset type  Enter/→ asset list  Esc/Q exit", test.width)
				comparisonFrame = strings.Join(lines, "\r\n")
			}
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(comparisonFrame)))
			if got != test.want {
				t.Fatalf("Go frame digest = %s, approved Python digest = %s\nframe:\n%s", got, test.want, frame)
			}
		})
	}
}

func TestBaselineWorkspacePluralParentStateGolden(t *testing.T) {
	raw := mustPythonOracleBaselineResult(t)
	parentAuth := cloneBaselineMap(raw["protections"].([]any)[0].(map[string]any))
	parentAuth["id"] = "prot:user_auth"
	parentAuth["asset_id"] = "obj:user"
	raw["protections"] = append(raw["protections"].([]any), parentAuth)
	workspace, err := NewBaselineWorkspace(raw, map[string]any{}, "/tmp/example-repo", "a8c4f21")
	if err != nil {
		t.Fatal(err)
	}
	workspace.color = false
	frame := workspace.renderBaselineFrame(baselineWorkspaceState{
		kindIndex: 2,
		focus:     baselineFocusAssets,
	}, 120, 50)
	if !strings.Contains(frame, "CONTROLS ON PARENT OBJECT  2") || strings.Contains(frame, "PARENT OBJECT CONTEXT") {
		t.Fatalf("plural-parent frame has incorrect copy:\n%s", frame)
	}
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(frame)))
	const want = "c2025a9ef642ec411655ad6ccf155c007e2522488a1a16e0a7141f13a0921cbc"
	if got != want {
		t.Fatalf("plural-parent frame digest = %s, want %s\nframe:\n%s", got, want, frame)
	}
}

func TestBaselineWorkspaceLongObjectMatchesApprovedPythonFrame(t *testing.T) {
	raw := mustPythonOracleBaselineResult(t)
	for _, value := range raw["assets"].([]any) {
		asset := value.(map[string]any)
		if asset["id"] == "obj:user" {
			asset["name"] = "User account object with an extraordinarily long catalog display name"
		}
	}
	for _, value := range raw["controls"].([]any) {
		control := value.(map[string]any)
		if control["id"] == "ctrl:tenant" {
			control["name"] = "tenant-scoped data access requiring an exceptionally long control name"
		}
	}
	workspace, err := NewBaselineWorkspace(raw, map[string]any{}, "/tmp/example-repo", "a8c4f21")
	if err != nil {
		t.Fatal(err)
	}
	workspace.color = true
	frame := workspace.renderBaselineFrame(baselineWorkspaceState{
		kindIndex: 1,
		focus:     baselineFocusDetail,
		detailTarget: baselineDetailTarget{
			kind:  baselineTargetAsset,
			id:    "field:user_email",
			rowID: "child:field:user_email",
		},
	}, 120, 50)
	if !strings.Contains(frame, "…") || !strings.Contains(frame, "\x1b[1;33m◐ Partial\x1b[0m") {
		t.Fatalf("long-object frame lost truncation or Partial styling:\n%s", frame)
	}
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(frame)))
	const want = "610d478d90d063ef85b9a691c05b80167a9577691a34d152ec4d4e76c815735b"
	if got != want {
		t.Fatalf("long-object Go digest = %s, approved Python digest = %s\nframe:\n%s", got, want, frame)
	}
}

func TestBaselinePresenceToneStylesOnlyTheWideBadge(t *testing.T) {
	style := baselineStyle{enabled: true}
	for _, test := range []struct {
		presence string
		badge    string
		code     string
	}{
		{presence: "present", badge: "● Present", code: "1;32"},
		{presence: "partial", badge: "◐ Partial", code: "1;33"},
		{presence: "absent", badge: "○ Absent", code: "2"},
	} {
		t.Run(test.presence, func(t *testing.T) {
			plain := "Control name  Property  "
			got := baselineTone(style, plain+test.badge, "control-"+test.presence)
			want := plain + "\x1b[" + test.code + "m" + test.badge + "\x1b[0m"
			if got != want {
				t.Fatalf("styled row = %q, want %q", got, want)
			}
		})
	}
}

func TestBaselineRouteAndCodeWrappingMatchPrototypeWidths(t *testing.T) {
	rows := baselineRouteRows("GET", "/users", 80)
	if len(rows) != 1 || rows[0].text != "GET       /users" {
		t.Fatalf("short route rows = %#v", rows)
	}

	rows = baselineRouteRows("POST", "/a/very/long/synthetic/endpoint/path · 2 definitions", 20)
	if len(rows) < 2 || !strings.HasPrefix(rows[0].text, "POST      ") {
		t.Fatalf("wrapped route rows = %#v", rows)
	}
	rows = baselineRouteRows("GET", "/some-long-endpoint", 20)
	wantRoutes := []baselinePanelRow{{text: "GET       /some-"}, {text: "long-endpoint"}}
	if !reflect.DeepEqual(rows, wantRoutes) {
		t.Fatalf("hyphenated route rows = %#v, want %#v", rows, wantRoutes)
	}
	for index, row := range rows {
		if visibleLen(row.text) > 20 {
			t.Fatalf("route row %d width = %d, want <= 20: %q", index, visibleLen(row.text), row.text)
		}
		if index > 0 && strings.HasPrefix(row.text, "POST      ") {
			t.Fatalf("continuation row repeated the method prefix: %#v", rows)
		}
	}

	codeRows := baselineCodeRows(strings.Repeat("x", 40), 20)
	if len(codeRows) != 3 || visibleLen(codeRows[0].text) != 18 {
		t.Fatalf("code rows = %#v, want 16 content cells plus two-cell indent", codeRows)
	}
	codeRows = baselineCodeRows("alpha beta longword", 20)
	wantCodeRows := []baselinePanelRow{
		{text: "  alpha beta ", tone: "code"},
		{text: "  longword", tone: "code"},
	}
	if !reflect.DeepEqual(codeRows, wantCodeRows) {
		t.Fatalf("word-aware code rows = %#v, want %#v", codeRows, wantCodeRows)
	}
	codeRows = baselineCodeRows("alpha very-long-token", 20)
	wantCodeRows = []baselinePanelRow{
		{text: "  alpha very-long-", tone: "code"},
		{text: "  token", tone: "code"},
	}
	if !reflect.DeepEqual(codeRows, wantCodeRows) {
		t.Fatalf("hyphen-aware code rows = %#v, want %#v", codeRows, wantCodeRows)
	}
	for _, test := range []struct {
		value string
		want  []string
	}{
		{value: "prefix e-mail-address", want: []string{"prefix ", "e-mail-", "address"}},
		{value: "prefix foo--barbaz", want: []string{"prefix foo--", "barbaz"}},
		{value: "prefix x-y-zoo", want: []string{"prefix x-y-", "zoo"}},
		{value: "abcdefghijk-longlonglonglong", want: []string{"abcdefghijk-", "longlonglong", "long"}},
		{value: "c-ivalonglonglong", want: []string{"c-", "ivalonglongl", "ong"}},
	} {
		if got := baselineWrapCodeLine(test.value, 12); !reflect.DeepEqual(got, test.want) {
			t.Errorf("baselineWrapCodeLine(%q) = %#v, want %#v", test.value, got, test.want)
		}
	}
	if got := baselinePythonWrap(" abcdefghijklmnopqrst", 20, true); !reflect.DeepEqual(got, []string{"abcdefghijklmnopqrst"}) {
		t.Errorf("leading-whitespace wrap = %#v, want preserved word", got)
	}
	codeRows = baselineCodeRows("a\tb", 20)
	wantCodeRows = []baselinePanelRow{{text: "  a       b", tone: "code"}}
	if !reflect.DeepEqual(codeRows, wantCodeRows) {
		t.Fatalf("tab-expanded code rows = %#v, want %#v", codeRows, wantCodeRows)
	}
	codeRows = baselineCodeRows("\talpha\n\tbeta", 20)
	wantCodeRows = []baselinePanelRow{
		{text: "  alpha", tone: "code"},
		{text: "  beta", tone: "code"},
	}
	if !reflect.DeepEqual(codeRows, wantCodeRows) {
		t.Fatalf("tab-dedented code rows = %#v, want %#v", codeRows, wantCodeRows)
	}
}

func TestBaselineWorkspaceDetailRowsPreserveReviewSemantics(t *testing.T) {
	workspace := mustSyntheticBaselineWorkspace(t)
	endpoints, _ := workspace.catalog.ReviewableAssets("endpoint")
	var users BaselineDiscovery
	for _, endpoint := range endpoints {
		if endpoint.ID == "ep:users" {
			users = endpoint
		}
	}
	endpointText := baselineRowsText(workspace.baselineAssetDetailRows(users, 80, false, baselineDetailTarget{}))
	for _, want := range []string{
		"ENDPOINTS  2",
		"Inventory for this group; controls above are not attributed to individual",
		"endpoints.",
		"POST      /users · 2 definitions",
	} {
		if !strings.Contains(endpointText, want) {
			t.Fatalf("endpoint details missing %q:\n%s", want, endpointText)
		}
	}

	objects, _ := workspace.catalog.ReviewableAssets("object")
	var fieldTarget baselineDetailTarget
	for _, target := range workspace.baselineDetailTargets(objects[0]) {
		if target.kind == baselineTargetAsset && target.id == "field:user_email" {
			fieldTarget = target
		}
	}
	objectRows := workspace.baselineAssetDetailRows(objects[0], 80, true, fieldTarget)
	objectText := baselineRowsText(objectRows)
	for _, want := range []string{"CONTROLS ON THIS OBJECT", "CONTROL ON CHILD FIELD  1", "email · 1 control"} {
		if !strings.Contains(objectText, want) {
			t.Fatalf("object details missing %q:\n%s", want, objectText)
		}
	}
	if baselineSelectedTargetRow(objectRows, fieldTarget) < 0 {
		t.Fatal("child field is not a selectable detail target")
	}

	fields, _ := workspace.catalog.ReviewableAssets("field")
	fieldText := baselineRowsText(workspace.baselineAssetDetailRows(fields[0], 80, false, baselineDetailTarget{}))
	if !strings.Contains(fieldText, "CONTROL ON PARENT OBJECT  1") || strings.Contains(fieldText, "Parent: ") {
		t.Fatalf("field parent controls are not rendered separately:\n%s", fieldText)
	}
}

func TestBaselineWorkspaceTargetsDistinguishSameControlAcrossScopes(t *testing.T) {
	raw := cloneSyntheticBaselineResult(t, syntheticBaselineResult())
	raw["protections"] = append(raw["protections"].([]any), map[string]any{
		"id":                     "prot:email_tenant",
		"asset_id":               "field:user_email",
		"control_id":             "ctrl:tenant",
		"implementation_ids":     []any{"impl:tenant"},
		"presence":               "present",
		"description":            "The tenant filter also applies to email access.",
		"evidence":               []any{},
		"checked":                []any{},
		"source_observation_ids": []any{"mech:synthetic"},
	})
	workspace, err := NewBaselineWorkspace(raw, syntheticBaselineBlueprint(), "stored-service", "")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := workspace.catalog.ReviewableAssets("object")
	if err != nil {
		t.Fatal(err)
	}

	var tenantTargets []baselineDetailTarget
	for _, target := range workspace.baselineDetailTargets(objects[0]) {
		if target.kind == baselineTargetControl && target.id == "ctrl:tenant" {
			tenantTargets = append(tenantTargets, target)
		}
	}
	if len(tenantTargets) != 2 || tenantTargets[0] == tenantTargets[1] || tenantTargets[0].rowID == tenantTargets[1].rowID {
		t.Fatalf("same-control targets = %#v, want two unique rows", tenantTargets)
	}

	rows := workspace.baselineAssetDetailRows(objects[0], 80, true, tenantTargets[1])
	selected := 0
	for _, row := range rows {
		if row.tone == "selected" {
			selected++
			if row.target != tenantTargets[1] {
				t.Fatalf("selected row target = %#v, want %#v", row.target, tenantTargets[1])
			}
		}
	}
	if selected != 1 {
		t.Fatalf("selected rows = %d, want exactly 1", selected)
	}
}

func TestBaselineWorkspaceSanitizesArtifactTextAndKeepsFrameWidth(t *testing.T) {
	raw := cloneSyntheticBaselineResult(t, syntheticBaselineResult())
	assets := raw["assets"].([]any)
	assets[1].(map[string]any)["name"] = "admin\x1b[2Jroutes\nspoof-" + strings.Repeat("very-long-", 12)
	blueprint := syntheticBaselineBlueprint()
	blueprint["repo"].(map[string]any)["name"] = "service\x1b[31m-api"
	workspace, err := NewBaselineWorkspace(raw, blueprint, "stored\x1b[Hrepo", "abc\x1b[0m")
	if err != nil {
		t.Fatal(err)
	}
	workspace.color = false
	state := baselineWorkspaceState{focus: baselineFocusAssets}
	frame := workspace.renderBaselineFrame(state, 92, 34)
	if strings.ContainsRune(frame, '\x1b') {
		t.Fatalf("uncolored frame contains an injected escape: %q", frame)
	}
	for _, line := range strings.Split(frame, "\r\n") {
		if visibleLen(line) > 92 {
			t.Fatalf("frame line width = %d, want <= 92: %q", visibleLen(line), line)
		}
	}
	if !strings.Contains(frame, "�") {
		t.Fatalf("sanitized frame did not visibly replace controls: %q", frame)
	}
	if !strings.Contains(frame, "…") {
		t.Fatalf("long asset name was not truncated: %q", frame)
	}
}

func TestBaselineAssetListAlignsOneTwoAndThreeDigitCounts(t *testing.T) {
	raw := map[string]any{
		"format_version":  float64(1),
		"repo":            "example/counts",
		"source":          map[string]any{"kind": "synthetic", "observation_count": float64(0)},
		"assets":          []any{},
		"controls":        []any{},
		"implementations": []any{},
		"protections":     []any{},
		"unresolved":      []any{},
	}
	addAsset := func(id, kind, name string) {
		record := map[string]any{
			"id": id, "kind": kind, "name": name, "decl": "synthetic.go#" + id,
			"origin": "resources", "source_ids": []any{id},
		}
		if kind == "endpoint" {
			record["routes"] = []any{}
		}
		if kind == "field" {
			record["parent"] = nil
		}
		raw["assets"] = append(raw["assets"].([]any), record)
	}
	for index := range 123 {
		raw["controls"] = append(raw["controls"].([]any), map[string]any{
			"id": fmt.Sprintf("ctrl:%03d", index), "name": fmt.Sprintf("control %03d", index),
			"description": "Synthetic count control.", "property": "authorization",
			"asvs": []any{}, "source_observation_ids": []any{},
		})
	}
	addProtection := func(assetID string, controlIndex, sequence int) {
		raw["protections"] = append(raw["protections"].([]any), map[string]any{
			"id":       fmt.Sprintf("prot:%s:%03d:%03d", strings.ReplaceAll(assetID, ":", "_"), controlIndex, sequence),
			"asset_id": assetID, "control_id": fmt.Sprintf("ctrl:%03d", controlIndex),
			"implementation_ids": []any{}, "presence": "present",
			"description": "Synthetic count association.", "evidence": []any{}, "checked": []any{},
			"source_observation_ids": []any{},
		})
	}
	for _, item := range []struct {
		id    string
		count int
	}{{id: "ep:123", count: 123}, {id: "ep:12", count: 12}, {id: "ep:1", count: 1}} {
		addAsset(item.id, "endpoint", "endpoint with a deliberately long catalog display name "+item.id)
		for index := range item.count {
			addProtection(item.id, index, index)
		}
	}
	for index := range 12 {
		id := fmt.Sprintf("obj:%03d", index)
		addAsset(id, "object", "object "+id)
		addProtection(id, 0, index)
	}
	for index := range 123 {
		id := fmt.Sprintf("field:%03d", index)
		addAsset(id, "field", "field "+id)
		addProtection(id, 0, index)
	}
	catalog, err := ParseBaselineCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := catalog.ReviewableAssets("endpoint")
	if err != nil {
		t.Fatal(err)
	}
	workspace := &BaselineWorkspace{catalog: catalog}
	rows := workspace.baselineAssetListRows(
		baselineWorkspaceState{focus: baselineFocusAssets},
		"endpoint",
		assets,
		catalog.ReviewableAssetCounts(),
		20,
		36,
	)
	text := baselineRowsText(rows)
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	const want = "147d5720e6d488a7be9680abc3cad85914f43745b5d45dd4e7c5d28c5bdf5381"
	if got != want {
		t.Fatalf("count-alignment rows digest = %s, want %s\nrows:\n%s", got, want, text)
	}
}

func mustSyntheticBaselineWorkspace(t *testing.T) *BaselineWorkspace {
	t.Helper()
	workspace, err := NewBaselineWorkspace(
		syntheticBaselineResult(),
		syntheticBaselineBlueprint(),
		"stored-service",
		"0123456789abcdef",
	)
	if err != nil {
		t.Fatal(err)
	}
	workspace.color = false
	return workspace
}

func mustPythonOracleBaselineWorkspace(t *testing.T) *BaselineWorkspace {
	t.Helper()
	raw := mustPythonOracleBaselineResult(t)
	workspace, err := NewBaselineWorkspace(
		raw,
		pythonOracleBaselineBlueprint(),
		"stored-service",
		"0123456789abcdef",
	)
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func mustPythonOracleBaselineResult(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/python_baseline_fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func pythonOracleBaselineBlueprint() map[string]any {
	return map[string]any{
		"repo": map[string]any{
			"name":    "service-api",
			"layout":  "single_component",
			"summary": "Synthetic service used for terminal tests.",
		},
		"metrics": map[string]any{"source_files": float64(320), "source_lines": float64(548000)},
		"components": []any{
			map[string]any{"name": "api", "kind": "service"},
			map[string]any{"name": "events", "kind": "worker"},
			map[string]any{"name": "console", "kind": "frontend"},
			map[string]any{"name": "shared models", "kind": "library"},
		},
		"languages":  []any{map[string]any{"name": "Go"}, map[string]any{"name": "TypeScript"}},
		"frameworks": []any{map[string]any{"name": "Chi"}, map[string]any{"name": "React"}},
		"orms":       []any{map[string]any{"name": "SQLC"}},
		"databases":  []any{map[string]any{"name": "PostgreSQL"}},
	}
}

func syntheticBaselineBlueprint() map[string]any {
	return map[string]any{
		"repo": map[string]any{
			"name":    "service-api",
			"layout":  "single_component",
			"summary": "Synthetic service used for terminal tests.",
		},
		"metrics": map[string]any{"source_files": float64(320), "source_lines": float64(548000)},
		"components": []any{
			map[string]any{"name": "api", "role": "service"},
			map[string]any{"name": "events", "role": "worker"},
			map[string]any{"name": "console", "role": "frontend"},
			map[string]any{"name": "shared models", "role": "library"},
		},
		"languages":  []any{map[string]any{"name": "Go"}, map[string]any{"name": "TypeScript"}},
		"frameworks": []any{map[string]any{"name": "Chi"}, map[string]any{"name": "React"}},
		"orms":       []any{map[string]any{"name": "SQLC"}},
		"databases":  []any{map[string]any{"name": "PostgreSQL"}},
	}
}

func reduceBaselineForTest(
	t *testing.T,
	workspace *BaselineWorkspace,
	state baselineWorkspaceState,
	key baselineKey,
) baselineWorkspaceState {
	t.Helper()
	next, transition, err := workspace.reduceBaselineState(state, key)
	if err != nil {
		t.Fatal(err)
	}
	if transition != baselineTransitionContinue {
		t.Fatalf("unexpected transition %v", transition)
	}
	return next
}

func baselineRowsText(rows []baselinePanelRow) string {
	values := make([]string, len(rows))
	for index, row := range rows {
		values[index] = row.text
	}
	return strings.Join(values, "\n")
}

func TestBaselineHelperStableOrderingAssumption(t *testing.T) {
	// This guards the synthetic fixture assumptions used by reducer tests.
	workspace := mustSyntheticBaselineWorkspace(t)
	assets, err := workspace.filteredBaselineAssets("endpoint", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{assets[0].ID, assets[1].ID}; !reflect.DeepEqual(got, []string{"ep:admin", "ep:users"}) {
		t.Fatalf("endpoint fixture order = %v", got)
	}
}
