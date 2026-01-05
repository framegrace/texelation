package widgets

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texelui/primitives"
)

func TestTabLayout_FocusTraversal(t *testing.T) {
	// Create a simple tab layout with two tabs
	tabs := []primitives.TabItem{
		{Label: "Tab1", ID: "tab1"},
		{Label: "Tab2", ID: "tab2"},
	}
	tl := NewTabLayout(0, 0, 80, 24, tabs)

	// Create content for tab 1 with two focusable inputs
	pane1 := NewPane()
	pane1.Resize(80, 20)
	input1 := NewInput()
	input1.Resize(20, 1)
	input2 := NewInput()
	input2.SetPosition(0, 1)
	input2.Resize(20, 1)
	pane1.AddChild(input1)
	pane1.AddChild(input2)
	tl.SetTabContent(0, pane1)

	// Create content for tab 2
	pane2 := NewPane()
	pane2.Resize(80, 20)
	input3 := NewInput()
	input3.Resize(20, 1)
	pane2.AddChild(input3)
	tl.SetTabContent(1, pane2)

	// Test 1: Focus() should correctly focus tab bar first (new behavior)
	t.Run("FocusSetupCorrectly", func(t *testing.T) {
		// Reset all focus state
		tl.Blur()
		input1.Blur()
		input2.Blur()
		tl.tabBar.Blur()

		// Only call Focus, don't manually set state
		tl.Focus()

		// Verify tab bar is focused (new behavior: focus starts at tab bar)
		if !tl.tabBar.IsFocused() {
			t.Error("After Focus(), tab bar should be focused")
		}

		// Verify focusArea is 0 (tab bar)
		if tl.focusArea != 0 {
			t.Errorf("After Focus(), focusArea should be 0, got %d", tl.focusArea)
		}

		// Verify input1 is not focused
		if input1.IsFocused() {
			t.Error("Input1 should not be focused when tab bar is focused")
		}
	})

	// Test 2: Tab from tab bar should go to content, then Shift-Tab back to tab bar
	t.Run("TabToContentAndShiftTabBack", func(t *testing.T) {
		// Reset and focus via Focus() only
		tl.Blur()
		input1.Blur()
		input2.Blur()
		tl.tabBar.Blur()
		tl.Focus()

		// Verify initial state: tab bar focused
		if !tl.tabBar.IsFocused() {
			t.Fatalf("Initial setup failed: tab bar not focused")
		}

		// Tab to content
		tabEv := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		handled := tl.HandleKey(tabEv)
		if !handled {
			t.Error("TabLayout should handle Tab from tab bar")
		}
		if !input1.IsFocused() {
			t.Error("After Tab from tab bar, input1 should be focused")
		}

		// Now Shift-Tab back to tab bar
		ev := tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		handled = tl.HandleKey(ev)

		if !handled {
			t.Error("TabLayout should handle Shift-Tab from first widget")
		}
		if tl.focusArea != 0 {
			t.Errorf("After Shift-Tab from first widget, focusArea should be 0 (tabBar), got %d", tl.focusArea)
		}
		if !tl.tabBar.IsFocused() {
			t.Error("Tab bar should be focused after Shift-Tab from first widget")
		}
		if input1.IsFocused() {
			t.Error("input1 should be blurred after Shift-Tab")
		}
	})

	// Test 3: Tab from tab bar should go to content
	t.Run("TabToContent", func(t *testing.T) {
		// Reset - focus tab bar directly
		tl.Blur()
		input1.Blur()
		input2.Blur()
		tl.tabBar.Blur()

		// Focus tab bar
		tl.tabBar.Focus()
		tl.focusArea = 0

		// Simulate Tab
		ev := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		handled := tl.HandleKey(ev)

		if !handled {
			t.Error("TabLayout should handle Tab from tab bar")
		}
		if tl.focusArea != 1 {
			t.Errorf("After Tab from tab bar, focusArea should be 1 (content), got %d", tl.focusArea)
		}
		if !input1.IsFocused() {
			t.Error("After Tab from tab bar, first input should be focused")
		}
		if tl.tabBar.IsFocused() {
			t.Error("Tab bar should be blurred after Tab to content")
		}
	})

	// Test 4: Tab within content should move between widgets
	t.Run("TabWithinContent", func(t *testing.T) {
		// Reset
		tl.Blur()
		input1.Blur()
		input2.Blur()
		tl.tabBar.Blur()

		// Focus first input
		input1.Focus()
		tl.focusArea = 1

		// Simulate Tab
		ev := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		handled := tl.HandleKey(ev)

		if !handled {
			t.Error("TabLayout should handle Tab within content")
		}
		if !input2.IsFocused() {
			t.Error("After Tab from input1, input2 should be focused")
		}
		if tl.focusArea != 1 {
			t.Errorf("focusArea should still be 1 (content), got %d", tl.focusArea)
		}
	})

	// Test 5: Left/Right on tab bar should switch tabs
	t.Run("ArrowKeysOnTabBar", func(t *testing.T) {
		// Reset
		tl.Blur()
		input1.Blur()
		input2.Blur()
		tl.tabBar.Blur()

		// Focus tab bar
		tl.tabBar.Focus()
		tl.focusArea = 0
		initialTab := tl.ActiveIndex()

		// Simulate Right arrow
		ev := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
		handled := tl.HandleKey(ev)

		if !handled {
			t.Error("TabLayout should handle Right arrow on tab bar")
		}
		if tl.ActiveIndex() == initialTab && len(tabs) > 1 {
			t.Error("Right arrow should change active tab")
		}
	})
}

// TestTabLayout_UIManagerTraversal tests that TabLayout properly handles
// focus cycling via CycleFocus interface
func TestTabLayout_UIManagerTraversal(t *testing.T) {
	tabs := []primitives.TabItem{
		{Label: "Tab1", ID: "tab1"},
	}
	tl := NewTabLayout(0, 0, 80, 24, tabs)

	pane := NewPane()
	pane.Resize(80, 20)
	input1 := NewInput()
	input1.Resize(20, 1)
	input2 := NewInput()
	input2.SetPosition(0, 1)
	input2.Resize(20, 1)
	pane.AddChild(input1)
	pane.AddChild(input2)
	tl.SetTabContent(0, pane)

	t.Run("CycleFocusForward", func(t *testing.T) {
		// Initial setup: focus TabLayout (focuses tab bar first)
		tl.Blur()
		tl.Focus()

		// Verify initial state: tab bar focused
		if !tl.tabBar.IsFocused() {
			t.Fatalf("Initial: tab bar should be focused")
		}
		if tl.focusArea != 0 {
			t.Fatalf("Initial: focusArea should be 0, got %d", tl.focusArea)
		}

		// CycleFocus forward should go to content
		tl.CycleFocus(true)

		if tl.focusArea != 1 {
			t.Errorf("After CycleFocus(true), focusArea should be 1 (content), got %d", tl.focusArea)
		}
		if !input1.IsFocused() {
			t.Error("After CycleFocus(true), input1 should be focused")
		}
		if tl.tabBar.IsFocused() {
			t.Error("Tab bar should not be focused after CycleFocus(true)")
		}
	})

	t.Run("TabFromTabBarToContent", func(t *testing.T) {
		// Start with tab bar focused
		tl.Blur()
		tl.tabBar.Focus()
		tl.focusArea = 0

		// Press Tab to go to content
		ev := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		handled := tl.HandleKey(ev)

		if !handled {
			t.Error("TabLayout should handle Tab from tab bar")
		}
		if tl.focusArea != 1 {
			t.Errorf("After Tab, focusArea should be 1, got %d", tl.focusArea)
		}
		if !input1.IsFocused() {
			t.Error("After Tab from tab bar, input1 should be focused")
		}
	})
}

func TestTabLayout_TabBarAlwaysAccessible(t *testing.T) {
	tabs := []primitives.TabItem{
		{Label: "Tab1", ID: "tab1"},
	}
	tl := NewTabLayout(0, 0, 80, 24, tabs)

	pane := NewPane()
	pane.Resize(80, 20)
	input := NewInput()
	input.Resize(20, 1)
	pane.AddChild(input)
	tl.SetTabContent(0, pane)

	// Focus should work on tab bar
	tl.tabBar.Focus()
	tl.focusArea = 0

	if !tl.tabBar.IsFocused() {
		t.Error("Tab bar should be focusable")
	}

	// Tab bar should handle left/right keys
	ev := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	// This should be handled (even if it doesn't change anything with 1 tab)
	tl.HandleKey(ev)
	// No crash = success
}
