// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/configeditor/configeditor.go
// Summary: TexelUI-based configuration editor for system, theme, and app configs.

package configeditor

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/config"
	"texelation/internal/effects"
	"texelation/registry"
	"texelation/texel"
	"texelation/texel/theme"
	"texelation/texelui/adapter"
	"texelation/texelui/core"
	"texelation/texelui/scroll"
	"texelation/texelui/widgets"
)

// Compile-time interface checks.
var _ texel.App = (*ConfigEditor)(nil)
var _ texel.ControlBusProvider = (*ConfigEditor)(nil)

type targetKind int

const (
	targetSystem targetKind = iota
	targetApp
)

type configTarget struct {
	kind           targetKind
	name           string
	label          string
	appOptions     []string
	values         config.Config
	themeValues    config.Config
	themeOverrides config.Config
	content        *targetContent   // Wrapper with header label + sections
	sections       *widgets.TabPanel
	bindings       []*fieldBinding
}

// targetContent wraps a header label and sections TabPanel for each target.
// Embeds Pane for focus/key/mouse handling, just adds layout on resize.
type targetContent struct {
	*widgets.Pane
	header   *widgets.Label
	sections *widgets.TabPanel
}

func newTargetContent(title string, sections *widgets.TabPanel) *targetContent {
	pane := widgets.NewPane(0, 0, 1, 1, tcell.StyleDefault)
	header := widgets.NewLabel(0, 0, 1, 1, title)
	pane.AddChild(header)
	pane.AddChild(sections)
	return &targetContent{
		Pane:     pane,
		header:   header,
		sections: sections,
	}
}

func (tc *targetContent) Resize(w, h int) {
	tc.Pane.Resize(w, h)
	// Layout: header at top, sections below
	tc.header.SetPosition(tc.Rect.X+2, tc.Rect.Y)
	tc.header.Resize(w-4, 1)
	contentH := h - 2
	if contentH < 1 {
		contentH = 1
	}
	tc.sections.SetPosition(tc.Rect.X, tc.Rect.Y+2)
	tc.sections.Resize(w, contentH)
}

type fieldKind int

const (
	fieldString fieldKind = iota
	fieldInt
	fieldFloat
	fieldBool
	fieldColor
	fieldJSON
	fieldCombo
)

type applyKind int

const (
	applySystem applyKind = iota
	applyTheme
	applyApp
	applyAppTheme
)

type fieldBinding struct {
	section string
	key     string
	kind    fieldKind
	widget  core.Widget
	err     error
}

// ConfigEditor is a TexelUI config editor app.
type ConfigEditor struct {
	*adapter.UIApp
	registry      *registry.Registry
	targets       []*configTarget
	rootTabs      *widgets.TabPanel
	activeWidget  core.Widget        // Currently displayed widget (rootTabs or single target.sections)
	defaultTarget string
	controlBus    texel.ControlBus
	autoApply     bool
	singleTarget  bool
	statusBar     *widgets.StatusBar // Cached to avoid lock during callbacks
}

// New creates a config editor app.
func New(reg *registry.Registry) texel.App {
	return NewWithTarget(reg, "")
}

// NewWithTarget creates a config editor app with an optional default target.
func NewWithTarget(reg *registry.Registry, target string) texel.App {
	ui := core.NewUIManager()
	uiApp := adapter.NewUIApp("Config Editor", ui)
	editor := &ConfigEditor{
		UIApp:         uiApp,
		registry:      reg,
		defaultTarget: target,
		controlBus:    texel.NewControlBus(),
		autoApply:     true,
		singleTarget:  target != "" && target != "system",
		statusBar:     uiApp.StatusBar(), // Cache before any locks are held
	}
	editor.buildTargets()
	editor.buildUI()
	return editor
}

// SetDefaultTarget updates the initial target selection.
func (e *ConfigEditor) SetDefaultTarget(name string) {
	e.defaultTarget = name
	e.singleTarget = name != "" && name != "system"
	e.applyRootMode()
}

// RegisterControl implements texel.ControlBusProvider.
func (e *ConfigEditor) RegisterControl(id, description string, handler func(payload interface{}) error) error {
	if e.controlBus == nil {
		e.controlBus = texel.NewControlBus()
	}
	return e.controlBus.Register(id, description, texel.ControlHandler(handler))
}

func (e *ConfigEditor) buildTargets() {
	e.targets = nil

	appEntries := e.appEntries()
	appOptions := make([]string, 0, len(appEntries))
	for _, entry := range appEntries {
		appOptions = append(appOptions, entry.name)
	}
	sort.Strings(appOptions)

	systemTarget := &configTarget{
		kind:        targetSystem,
		name:        "system",
		label:       "System",
		appOptions:  appOptions,
		values:      ensureConfig(config.Clone(config.System())),
		themeValues: ensureConfig(cloneThemeConfig()),
	}
	e.targets = append(e.targets, systemTarget)

	for _, entry := range appEntries {
		values := ensureConfig(config.Clone(config.App(entry.name)))
		overrides := cloneThemeOverrides(values["theme_overrides"])
		if len(overrides) > 0 {
			values["theme_overrides"] = overrides
		}
		e.targets = append(e.targets, &configTarget{
			kind:           targetApp,
			name:           entry.name,
			label:          entry.label,
			values:         values,
			themeOverrides: overrides,
		})
	}
}

type appEntry struct {
	name  string
	label string
}

func (e *ConfigEditor) appEntries() []appEntry {
	entries := make([]appEntry, 0)
	if e.registry != nil {
		for _, app := range e.registry.List() {
			if app.Manifest == nil {
				continue
			}
			name := app.Manifest.Name
			if name == "" || name == "config-editor" || name == "welcome" {
				continue
			}
			label := app.Manifest.DisplayName
			if label == "" {
				label = humanLabel(name)
			}
			entries = append(entries, appEntry{name: name, label: label})
		}
	}
	if len(entries) == 0 {
		for _, name := range []string{"texelterm", "launcher", "help", "statusbar", "clock"} {
			entries = append(entries, appEntry{name: name, label: humanLabel(name)})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].label < entries[j].label
	})
	return entries
}

func (e *ConfigEditor) buildUI() {
	e.rootTabs = e.buildTabs()
	e.applyRootMode()
}

func (e *ConfigEditor) Resize(cols, rows int) {
	e.UIApp.Resize(cols, rows)
	if e.activeWidget != nil {
		contentH := e.UI().ContentHeight()
		e.activeWidget.SetPosition(0, 0)
		e.activeWidget.Resize(cols, contentH)
	}
}

func (e *ConfigEditor) buildTabs() *widgets.TabPanel {
	panel := widgets.NewTabPanel(0, 0, 1, 1)
	for _, target := range e.targets {
		target.sections = e.buildSections(target)
		target.content = newTargetContent(target.label+" Configuration", target.sections)
		panel.AddTabWithID(target.label, target.name, target.content)
	}
	return panel
}

func (e *ConfigEditor) selectTarget(name string) {
	if name == "" || e.rootTabs == nil || e.singleTarget {
		return
	}
	for idx, target := range e.targets {
		if target.name == name {
			e.rootTabs.SetActive(idx)
			return
		}
	}
}

func (e *ConfigEditor) applyRootMode() {
	ui := e.UI()

	// Only add widget on first call (during buildUI)
	if e.activeWidget != nil {
		// Already set up, just update focus
		if e.singleTarget {
			target := e.targetByName(e.defaultTarget)
			if target != nil && target.content != nil {
				ui.Focus(target.content)
			}
		} else {
			e.selectTarget(e.defaultTarget)
			if e.rootTabs != nil {
				ui.Focus(e.rootTabs)
			}
		}
		return
	}

	if e.singleTarget {
		target := e.targetByName(e.defaultTarget)
		if target != nil && target.content != nil {
			e.activeWidget = target.content
			ui.AddWidget(e.activeWidget)
			ui.Focus(target.content)
			return
		}
	}

	// Multi-target mode: show root tabs
	e.activeWidget = e.rootTabs
	ui.AddWidget(e.activeWidget)
	e.selectTarget(e.defaultTarget)
	if e.rootTabs != nil {
		ui.Focus(e.rootTabs)
	}
}

func (e *ConfigEditor) targetByName(name string) *configTarget {
	if name == "" {
		return nil
	}
	for _, target := range e.targets {
		if target.name == name {
			return target
		}
	}
	return nil
}

func (e *ConfigEditor) buildSections(target *configTarget) *widgets.TabPanel {
	switch target.kind {
	case targetSystem:
		return e.buildSystemSections(target)
	case targetApp:
		return e.buildAppSections(target)
	default:
		return widgets.NewTabPanel(0, 0, 1, 1)
	}
}

func (e *ConfigEditor) buildSystemSections(target *configTarget) *widgets.TabPanel {
	panel := widgets.NewTabPanel(0, 0, 1, 1)
	target.bindings = nil

	generalValues := generalValues(target.values)
	panel.AddTab("General", e.buildSectionPane(target, target.values, "", generalValues, false, applySystem))

	layoutValues := sectionValues(target.values, "layout_transitions")
	panel.AddTab("Layout Transitions", e.buildSectionPane(target, target.values, "layout_transitions", layoutValues, false, applySystem))

	effectsValues := sectionValues(target.values, "effects")
	panel.AddTab("Effects", e.buildEffectsSection(target, effectsValues))

	themePane := e.buildGroupedThemePane(target, target.themeValues, systemThemeSections, true)
	panel.AddTab("Theme", themePane)

	uiValues := sectionValues(target.themeValues, "ui")
	panel.AddTab("TexelUI Theme", e.buildSectionPane(target, target.themeValues, "ui", uiValues, true, applyTheme))

	return panel
}

func (e *ConfigEditor) buildAppSections(target *configTarget) *widgets.TabPanel {
	sections := splitSections(target.values)
	delete(sections, "theme_overrides")
	if len(sections) == 0 {
		sections[""] = map[string]interface{}{}
	}
	sectionKeys := make([]string, 0, len(sections))
	for key := range sections {
		sectionKeys = append(sectionKeys, key)
	}
	sort.Strings(sectionKeys)

	panel := widgets.NewTabPanel(0, 0, 1, 1)
	target.bindings = nil
	for _, key := range sectionKeys {
		label := key
		if key == "" {
			label = "General"
		} else {
			label = humanLabel(key)
		}
		pane := e.buildSectionPane(target, target.values, key, sections[key], false, applyApp)
		panel.AddTab(label, pane)
	}
	themePane := e.buildAppThemePane(target)
	panel.AddTab("Theme", themePane)
	return panel
}

func (e *ConfigEditor) buildSectionPane(target *configTarget, cfg config.Config, sectionKey string, values map[string]interface{}, forceColor bool, apply applyKind) core.Widget {
	pane := newFormPane(0, 0, 1, 1)
	if values == nil {
		values = make(map[string]interface{})
	}
	if target.kind == targetSystem && sectionKey == "effects" {
		return e.buildEffectsSection(target, values)
	}
	keys := keysSorted(values)
	for _, key := range keys {
		value := values[key]
		binding := e.buildField(cfg, target, sectionKey, key, value, pane, forceColor, apply)
		if binding != nil {
			target.bindings = append(target.bindings, binding)
		}
	}
	return wrapInScrollPane(pane)
}

func (e *ConfigEditor) buildField(cfg config.Config, target *configTarget, sectionKey, key string, value interface{}, pane *formPane, forceColor bool, apply applyKind) *fieldBinding {
	fb := NewFieldBuilder(target, cfg, func(kind applyKind) {
		e.applyTargetConfig(target, kind)
	})
	return fb.Build(FieldConfig{
		Section:    sectionKey,
		Key:        key,
		Value:      value,
		ForceColor: forceColor,
		ApplyKind:  apply,
	}, pane)
}

type effectBinding struct {
	Event  string
	Target string
	Effect string
	Params map[string]interface{}
}

func (e *ConfigEditor) buildEffectsSection(target *configTarget, values map[string]interface{}) core.Widget {
	pane := newFormPane(0, 0, 1, 1)
	rawBindings := values["bindings"]
	bindings := parseEffectBindings(rawBindings)

	events := effects.TriggerNames()
	events = append(events, extraEffectEvents(events, bindings)...)
	options := effectOptions()

	for _, event := range events {
		label := widgets.NewLabel(0, 0, 0, 1, humanLabel(event))
		combo := widgets.NewComboBox(0, 0, 0, options, false)

		selected := noneEffectLabel
		if binding, ok := bindings[event]; ok && binding.Effect != "" {
			selected = binding.Effect
		}
		combo.SetValue(selected)

		eventName := event
		combo.OnChange = func(value string) {
			if value == noneEffectLabel || value == "" {
				delete(bindings, eventName)
			} else {
				binding := bindings[eventName]
				if binding == nil {
					binding = &effectBinding{
						Event:  eventName,
						Target: defaultTargetForEvent(eventName),
					}
					bindings[eventName] = binding
				}
				binding.Effect = value
				if binding.Target == "" {
					binding.Target = defaultTargetForEvent(eventName)
				}
			}
			updateConfigValue(target.values, "effects", "bindings", bindingsToConfig(bindings, events))
			e.applyTargetConfig(target, applySystem)
		}

		pane.AddRow(formRow{label: label, field: combo, height: 1})
	}
	return wrapInScrollPane(pane)
}

func (e *ConfigEditor) applyTargetConfig(target *configTarget, kind applyKind) {
	if target == nil {
		return
	}
	var err error
	switch kind {
	case applySystem:
		config.SetSystem(target.values)
		err = config.SaveSystem()
	case applyTheme:
		err = saveThemeConfig(target.themeValues)
	case applyApp:
		config.SetApp(target.name, target.values)
		err = config.SaveApp(target.name)
	case applyAppTheme:
		syncThemeOverrides(target)
		config.SetApp(target.name, target.values)
		err = config.SaveApp(target.name)
	}

	if err != nil {
		e.showError(fmt.Sprintf("Apply failed: %v", err))
	} else {
		e.showSuccess("Saved.")
	}

	if err == nil {
		e.emitApply(kind, target)
	}
}

// showSuccess displays a success message in the global StatusBar.
// Uses cached StatusBar reference to avoid acquiring UIManager.mu during callbacks.
func (e *ConfigEditor) showSuccess(msg string) {
	if e.statusBar != nil {
		e.statusBar.ShowSuccess(msg)
	}
}

// showError displays an error message in the global StatusBar.
// Uses cached StatusBar reference to avoid acquiring UIManager.mu during callbacks.
func (e *ConfigEditor) showError(msg string) {
	if e.statusBar != nil {
		e.statusBar.ShowError(msg)
	}
}

func (e *ConfigEditor) emitApply(kind applyKind, target *configTarget) {
	if e.controlBus == nil {
		return
	}
	payload := applyPayload(kind, target)
	if payload == "" {
		return
	}
	// Trigger asynchronously to avoid blocking the key handler.
	// The control bus handler (in Desktop) can do synchronous network I/O,
	// which would freeze the UI if triggered from within HandleKey.
	go func() {
		_ = e.controlBus.Trigger("config-editor.apply", payload)
	}()
}

func (e *ConfigEditor) buildGroupedThemePane(target *configTarget, cfg config.Config, sections []string, forceColor bool) core.Widget {
	pane := newFormPane(0, 0, 1, 1)
	if cfg == nil {
		cfg = make(config.Config)
	}
	first := true
	for _, sectionKey := range sections {
		values := sectionValues(cfg, sectionKey)
		if len(values) == 0 {
			continue
		}
		if !first {
			pane.AddRow(formRow{height: 1})
		}
		header := newSectionHeader(humanLabel(sectionKey))
		pane.AddRow(formRow{field: header, height: 1, fullWidth: true})
		keys := keysSorted(values)
		for _, key := range keys {
			value := values[key]
			binding := e.buildField(cfg, target, sectionKey, key, value, pane, forceColor, applyTheme)
			if binding != nil {
				target.bindings = append(target.bindings, binding)
			}
		}
		first = false
	}
	return wrapInScrollPane(pane)
}

func (e *ConfigEditor) buildAppThemePane(target *configTarget) core.Widget {
	pane := newFormPane(0, 0, 1, 1)
	base := ensureConfig(cloneThemeConfig())
	effective := mergeThemeConfig(base, target.themeOverrides)
	fields := appThemeFields(target.name)
	fields = filterAppThemeFields(fields, base, target.themeOverrides)
	if len(fields) == 0 {
		fields = overrideThemeFields(target.themeOverrides)
	}
	if len(fields) == 0 {
		pane.AddRow(formRow{field: widgets.NewLabel(0, 0, 0, 1, "No theme settings for this app."), height: 1, fullWidth: true})
		return wrapInScrollPane(pane)
	}

	sectionKeys := make([]string, 0, len(fields))
	for key := range fields {
		sectionKeys = append(sectionKeys, key)
	}
	sort.Strings(sectionKeys)
	first := true
	for _, sectionKey := range sectionKeys {
		keys := fields[sectionKey]
		if len(keys) == 0 {
			continue
		}
		if !first {
			pane.AddRow(formRow{height: 1})
		}
		header := newSectionHeader(humanLabel(sectionKey))
		pane.AddRow(formRow{field: header, height: 1, fullWidth: true})
		sort.Strings(keys)
		for _, key := range keys {
			rawValue, ok := themeValue(effective, sectionKey, key)
			if !ok {
				continue
			}
			strValue, ok := rawValue.(string)
			if !ok {
				continue
			}
			label := humanLabel(key)
			colorPicker := widgets.NewColorPicker(0, 0, widgets.ColorPickerConfig{
				EnableSemantic: true,
				EnablePalette:  true,
				EnableOKLCH:    true,
				Label:          label,
			})
			colorPicker.SetValue(strValue)

			sectionName := sectionKey
			fieldKey := key
			baseValue, _ := themeValue(base, sectionKey, key)
			colorPicker.OnChange = func(result widgets.ColorPickerResult) {
				target.themeOverrides = setThemeOverride(target.themeOverrides, sectionName, fieldKey, result.Source, baseValue)
				syncThemeOverrides(target)
				e.applyTargetConfig(target, applyAppTheme)
			}
			pane.AddRow(formRow{label: widgets.NewLabel(0, 0, 0, 1, label), field: colorPicker, height: 1})
		}
		first = false
	}
	return wrapInScrollPane(pane)
}

func newSectionHeader(text string) *widgets.Label {
	label := widgets.NewLabel(0, 0, 0, 1, text)
	label.Style = label.Style.Bold(true)
	return label
}

func ensureConfig(cfg config.Config) config.Config {
	if cfg == nil {
		return make(config.Config)
	}
	return cfg
}

func generalValues(cfg config.Config) map[string]interface{} {
	values := make(map[string]interface{})
	if cfg == nil {
		return values
	}
	for key, value := range cfg {
		switch value.(type) {
		case map[string]interface{}, config.Section:
			continue
		default:
			values[key] = value
		}
	}
	return values
}

func sectionValues(cfg config.Config, sectionKey string) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{}
	}
	section := cfg.Section(sectionKey)
	if section == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}(section)
}

func mergeThemeConfig(base config.Config, overrides config.Config) config.Config {
	merged := config.Clone(base)
	if merged == nil {
		merged = make(config.Config)
	}
	if overrides == nil {
		return merged
	}
	for sectionName, rawSection := range overrides {
		section, ok := rawSection.(map[string]interface{})
		if !ok {
			if s, ok := rawSection.(config.Section); ok {
				section = map[string]interface{}(s)
			}
		}
		if section == nil {
			continue
		}
		dest := merged.Section(sectionName)
		if dest == nil {
			dest = make(config.Section)
			merged[sectionName] = dest
		}
		for key, value := range section {
			dest[key] = value
		}
	}
	return merged
}

func themeValue(cfg config.Config, sectionName, key string) (interface{}, bool) {
	if cfg == nil {
		return nil, false
	}
	section := cfg.Section(sectionName)
	if section == nil {
		return nil, false
	}
	value, ok := section[key]
	return value, ok
}

func appThemeFields(app string) map[string][]string {
	switch app {
	case "texelterm":
		return map[string][]string{
			"selection": {"highlight_bg", "highlight_fg"},
			"ui":        {"text.primary", "bg.base"},
		}
	case "launcher":
		return map[string][]string{
			"ui": {"bg.surface", "text.primary", "text.inverse", "accent.primary"},
		}
	case "help":
		return map[string][]string{
			"desktop": {"default_bg"},
			"ui":      {"text.primary", "text.secondary", "text.active"},
		}
	case "statusbar":
		return map[string][]string{
			"ui": {"bg.mantle", "text.primary", "action.danger", "text.inverse", "bg.crust", "text.muted"},
		}
	default:
		return nil
	}
}

func filterAppThemeFields(fields map[string][]string, base config.Config, overrides config.Config) map[string][]string {
	if len(fields) == 0 {
		return nil
	}
	filtered := make(map[string][]string)
	for sectionName, keys := range fields {
		if len(keys) == 0 {
			continue
		}
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			if _, ok := themeValue(base, sectionName, key); ok {
				out = append(out, key)
				continue
			}
			if _, ok := themeValue(overrides, sectionName, key); ok {
				out = append(out, key)
			}
		}
		if len(out) > 0 {
			filtered[sectionName] = out
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func overrideThemeFields(overrides config.Config) map[string][]string {
	if overrides == nil {
		return nil
	}
	out := make(map[string][]string)
	for sectionName, rawSection := range overrides {
		section, ok := rawSection.(map[string]interface{})
		if !ok {
			if s, ok := rawSection.(config.Section); ok {
				section = map[string]interface{}(s)
			}
		}
		if len(section) == 0 {
			continue
		}
		keys := make([]string, 0, len(section))
		for key := range section {
			keys = append(keys, key)
		}
		if len(keys) > 0 {
			out[sectionName] = keys
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func setThemeOverride(overrides config.Config, sectionName, key string, value interface{}, baseValue interface{}) config.Config {
	if overrides == nil {
		overrides = make(config.Config)
	}
	if baseValue != nil && reflect.DeepEqual(value, baseValue) {
		removeThemeOverride(overrides, sectionName, key)
		return overrides
	}
	section := overrides.Section(sectionName)
	if section == nil {
		section = make(config.Section)
		overrides[sectionName] = section
	}
	section[key] = value
	return overrides
}

func removeThemeOverride(overrides config.Config, sectionName, key string) {
	if overrides == nil {
		return
	}
	section := overrides.Section(sectionName)
	if section == nil {
		return
	}
	delete(section, key)
	if len(section) == 0 {
		delete(overrides, sectionName)
	}
}

func syncThemeOverrides(target *configTarget) {
	if target == nil {
		return
	}
	if target.values == nil {
		target.values = make(config.Config)
	}
	target.themeOverrides = pruneOverrides(target.themeOverrides)
	if len(target.themeOverrides) == 0 {
		delete(target.values, "theme_overrides")
		return
	}
	target.values["theme_overrides"] = target.themeOverrides
}

func pruneOverrides(overrides config.Config) config.Config {
	if overrides == nil {
		return nil
	}
	for sectionName, rawSection := range overrides {
		section, ok := rawSection.(map[string]interface{})
		if !ok {
			if s, ok := rawSection.(config.Section); ok {
				section = map[string]interface{}(s)
			}
		}
		if len(section) == 0 {
			delete(overrides, sectionName)
		}
	}
	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

func cloneThemeOverrides(raw interface{}) config.Config {
	parsed := theme.ParseOverrides(raw)
	if len(parsed) == 0 {
		return nil
	}
	out := make(config.Config, len(parsed))
	for sectionName, section := range parsed {
		copySection := make(map[string]interface{}, len(section))
		for key, value := range section {
			copySection[key] = value
		}
		out[sectionName] = copySection
	}
	return out
}

func applyPayload(kind applyKind, target *configTarget) string {
	switch kind {
	case applySystem:
		return "system"
	case applyTheme:
		return "theme"
	case applyApp:
		if target != nil && target.name != "" {
			return "app:" + target.name
		}
	case applyAppTheme:
		if target != nil && target.name != "" {
			return "app-theme:" + target.name
		}
	}
	return ""
}

func (e *ConfigEditor) saveTarget(target *configTarget) error {
	switch target.kind {
	case targetSystem:
		config.SetSystem(target.values)
		if err := config.SaveSystem(); err != nil {
			return err
		}
		if err := config.ReloadSystem(); err != nil {
			return err
		}
		if target.themeValues != nil {
			if err := saveThemeConfig(target.themeValues); err != nil {
				return err
			}
		}
		return nil
	case targetApp:
		syncThemeOverrides(target)
		config.SetApp(target.name, target.values)
		if err := config.SaveApp(target.name); err != nil {
			return err
		}
		return config.ReloadApp(target.name)
	default:
		return nil
	}
}

func (e *ConfigEditor) reloadTarget(target *configTarget) error {
	switch target.kind {
	case targetSystem:
		if err := config.ReloadSystem(); err != nil {
			return err
		}
		target.values = ensureConfig(config.Clone(config.System()))
		if err := theme.Reload(); err != nil {
			return err
		}
		target.themeValues = ensureConfig(cloneThemeConfig())
	case targetApp:
		if err := config.ReloadApp(target.name); err != nil {
			return err
		}
		target.values = ensureConfig(config.Clone(config.App(target.name)))
		target.themeOverrides = cloneThemeOverrides(target.values["theme_overrides"])
		if len(target.themeOverrides) > 0 {
			target.values["theme_overrides"] = target.themeOverrides
		} else {
			delete(target.values, "theme_overrides")
		}
	}
	target.sections = e.buildSections(target)
	target.content = newTargetContent(target.label+" Configuration", target.sections)
	// Update the tab content in rootTabs
	if e.rootTabs != nil {
		for idx, t := range e.targets {
			if t == target {
				e.rootTabs.SetTabContent(idx, target.content)
				break
			}
		}
	}
	return nil
}

func splitSections(cfg config.Config) map[string]map[string]interface{} {
	sections := make(map[string]map[string]interface{})
	if cfg == nil {
		return sections
	}
	for key, value := range cfg {
		switch section := value.(type) {
		case map[string]interface{}:
			sections[key] = section
		case config.Section:
			sections[key] = map[string]interface{}(section)
		default:
			sec := sections[""]
			if sec == nil {
				sec = make(map[string]interface{})
				sections[""] = sec
			}
			sec[key] = value
		}
	}
	if len(sections) == 0 {
		sections[""] = map[string]interface{}{}
	}
	return sections
}

func updateConfigValue(cfg config.Config, section, key string, value interface{}) {
	if section == "" {
		cfg[key] = value
		return
	}
	sec := cfg.Section(section)
	if sec == nil {
		sec = make(config.Section)
		cfg[section] = sec
	}
	sec[key] = value
}

func formatJSON(value interface{}) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func formatNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func looksLikeColor(value string) bool {
	if strings.HasPrefix(value, "#") && len(value) >= 4 {
		return true
	}
	if strings.HasPrefix(value, "@") {
		return true
	}
	if strings.Contains(value, ".") {
		return true
	}
	return false
}

// comboOptions is an alias for ComboOptionsFor for internal use.
func comboOptions(target *configTarget, section, key string) []string {
	return ComboOptionsFor(target, section, key)
}

func cloneThemeConfig() config.Config {
	base := theme.Get()
	out := make(config.Config)
	for key, section := range base {
		sec := make(map[string]interface{}, len(section))
		for field, value := range section {
			sec[field] = value
		}
		out[key] = sec
	}
	return out
}

func saveThemeConfig(cfg config.Config) error {
	if cfg == nil {
		return nil
	}
	out := make(theme.Config)
	for key, section := range cfg {
		if sectionMap, ok := section.(map[string]interface{}); ok {
			out[key] = theme.Section(sectionMap)
		} else if sectionMap, ok := section.(config.Section); ok {
			out[key] = theme.Section(sectionMap)
		}
	}
	if err := out.Save(); err != nil {
		return err
	}
	return theme.Reload()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wrapInScrollPane wraps a formPane in a ScrollPane for scrollable content.
func wrapInScrollPane(pane *formPane) *scrollableForm {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	fg := tm.GetSemanticColor("text.primary")
	style := tcell.StyleDefault.Background(bg).Foreground(fg)

	contentH := pane.ContentHeight()
	pane.Resize(80, contentH) // Initial reasonable width, will be updated on resize

	sp := scroll.NewScrollPane(0, 0, 1, 1, style)
	sp.SetChild(pane)
	sp.SetContentHeight(contentH)

	return &scrollableForm{ScrollPane: sp, form: pane}
}

// scrollableForm wraps ScrollPane to resize child width on resize.
type scrollableForm struct {
	*scroll.ScrollPane
	form *formPane
}

func (sf *scrollableForm) Resize(w, h int) {
	sf.ScrollPane.Resize(w, h)
	// Resize form width to match viewport, but keep content height
	if sf.form != nil {
		sf.form.Resize(w, sf.form.ContentHeight())
		sf.ScrollPane.SetContentHeight(sf.form.ContentHeight())
	}
}

func (sf *scrollableForm) SetPosition(x, y int) {
	sf.ScrollPane.SetPosition(x, y)
}

// Focus delegates to the embedded ScrollPane.
func (sf *scrollableForm) Focus() {
	sf.ScrollPane.Focus()
}

// Blur delegates to the embedded ScrollPane.
func (sf *scrollableForm) Blur() {
	sf.ScrollPane.Blur()
}

// CycleFocus delegates to the embedded ScrollPane.
func (sf *scrollableForm) CycleFocus(forward bool) bool {
	return sf.ScrollPane.CycleFocus(forward)
}

// TrapsFocus delegates to the embedded ScrollPane.
func (sf *scrollableForm) TrapsFocus() bool {
	return sf.ScrollPane.TrapsFocus()
}

// VisitChildren delegates to the embedded ScrollPane.
func (sf *scrollableForm) VisitChildren(f func(core.Widget)) {
	sf.ScrollPane.VisitChildren(f)
}

const noneEffectLabel = "(none)"

var systemThemeSections = []string{
	"desktop",
	"pane",
	"selection",
	"statusbar",
	"clock",
}

func effectOptions() []string {
	ids := effects.RegisteredIDs()
	sort.Strings(ids)
	options := make([]string, 0, len(ids)+1)
	options = append(options, noneEffectLabel)
	options = append(options, ids...)
	return options
}

func parseEffectBindings(raw interface{}) map[string]*effectBinding {
	entries := parseBindingsRaw(raw)
	if len(entries) == 0 {
		return make(map[string]*effectBinding)
	}
	out := make(map[string]*effectBinding)
	for _, entry := range entries {
		event, _ := entry["event"].(string)
		if event == "" {
			continue
		}
		if _, exists := out[event]; exists {
			continue
		}
		target, _ := entry["target"].(string)
		effect, _ := entry["effect"].(string)
		var params map[string]interface{}
		if rawParams, ok := entry["params"].(map[string]interface{}); ok {
			params = rawParams
		}
		out[event] = &effectBinding{
			Event:  event,
			Target: target,
			Effect: effect,
			Params: params,
		}
	}
	return out
}

func parseBindingsRaw(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case nil:
		return nil
	case []map[string]interface{}:
		return v
	case []interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var out []map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		var out []map[string]interface{}
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}

func bindingsToConfig(bindings map[string]*effectBinding, events []string) []map[string]interface{} {
	seen := make(map[string]struct{}, len(events))
	result := make([]map[string]interface{}, 0, len(bindings))
	for _, event := range events {
		seen[event] = struct{}{}
		if binding, ok := bindings[event]; ok {
			if binding.Effect == "" {
				continue
			}
			entry := map[string]interface{}{
				"event":  binding.Event,
				"target": binding.Target,
				"effect": binding.Effect,
			}
			if len(binding.Params) > 0 {
				entry["params"] = binding.Params
			}
			result = append(result, entry)
		}
	}
	extraEvents := make([]string, 0)
	for event := range bindings {
		if _, ok := seen[event]; !ok {
			extraEvents = append(extraEvents, event)
		}
	}
	sort.Strings(extraEvents)
	for _, event := range extraEvents {
		binding := bindings[event]
		if binding == nil || binding.Effect == "" {
			continue
		}
		entry := map[string]interface{}{
			"event":  binding.Event,
			"target": binding.Target,
			"effect": binding.Effect,
		}
		if len(binding.Params) > 0 {
			entry["params"] = binding.Params
		}
		result = append(result, entry)
	}
	return result
}

func extraEffectEvents(events []string, bindings map[string]*effectBinding) []string {
	known := make(map[string]struct{}, len(events))
	for _, event := range events {
		known[event] = struct{}{}
	}
	var extra []string
	for event := range bindings {
		if _, ok := known[event]; !ok {
			extra = append(extra, event)
		}
	}
	sort.Strings(extra)
	return extra
}

func defaultTargetForEvent(event string) string {
	if strings.HasPrefix(event, "pane.") {
		return "pane"
	}
	return "workspace"
}
