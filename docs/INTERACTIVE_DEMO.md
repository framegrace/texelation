# Interactive TView Demo - User Guide

## Starting the Demo

### Terminal 1 - Start the Server
```bash
make server
```

You should see:
```
Texel server harness listening on /tmp/texelation.sock
Use the integration test client or proto harness to connect and send key events.
```

### Terminal 2 - Connect the Client
```bash
make client
```

The interactive demo will appear immediately!

## Screen Layout

```
┌────────────────────────────────────────────────────────────────────────┐
│            Interactive TView Demo                                      │
│            Use Tab to switch focus, Arrow keys to navigate             │
├─────────────────────────────────┬──────────────────────────────────────┤
│ ┌─ Options ─────────────────┐   │ ┌─ Dynamic Table ──────────────────┐│
│ │ 1. Start Process          │   │ │ Property  │ Value      │ Status  ││
│ │ 2. View Status            │   │ │ Counter   │ 0          │ Active  ││
│ │ 3. Configure Settings     │   │ │ Selected  │            │ OK      ││
│ │ 4. Run Tests              │   │ │ Timestamp │ 17:58:11   │ Current ││
│ │ 5. Clear Log              │   │ │ Mode      │ Interactive│ Enabled ││
│ │ 6. Toggle Mode            │   │ └──────────────────────────────────┘│
│ │ 7. Refresh Data           │   │                                      │
│ │ 8. Export Results         │   │ ┌─ Event Log ──────────────────────┐│
│ └───────────────────────────┘   │ │ 17:58:11 Interactive demo started││
├─────────────────────────────────┤ │ 17:58:11 Use Tab to switch widgets││
│ ┌─ Interactive Form ────────┐   │ │                                   ││
│ │ Name: [              ]    │   │ │                                   ││
│ │ Password: [***       ]    │   │ │                                   ││
│ │ Priority: [Medium    ▼]   │   │ │                                   ││
│ │ [✓] Enable notifications  │   │ │                                   ││
│ │ [ Submit ] [ Reset ]      │   │ └───────────────────────────────────┘│
│ └───────────────────────────┘   │                                      │
└─────────────────────────────────┴──────────────────────────────────────┘
```

## Controls

### Global Navigation
- **Tab** - Move focus to next widget
- **Shift+Tab** - Move focus to previous widget
- **Ctrl+Q** - Quit application

### Pane Navigation (Texelation)
- **Shift+Arrow** - Navigate between panes
- **Ctrl+A** then **|** - Split vertically
- **Ctrl+A** then **-** - Split horizontally
- **Ctrl+A** then **z** - Zoom/unzoom pane
- **Ctrl+A** then **x** - Close pane

### Options List (Top Left)
- **↑/↓ Arrow keys** - Navigate up/down
- **Enter** - Select highlighted item
- **1-8** - Quick select by number
- **Home** - Jump to first item
- **End** - Jump to last item

**What happens:**
- Selected item is logged in Event Log
- "Selected" field in Dynamic Table updates
- Counter increments

### Interactive Form (Bottom Left)

#### Name Field
- **Type** - Enter your name (max 20 chars)
- **Backspace** - Delete characters
- **Left/Right arrows** - Move cursor

#### Password Field
- **Type** - Enter password (shown as ***)
- **Backspace** - Delete characters

#### Priority Dropdown
- **Enter** or **Space** - Open dropdown
- **↑/↓ Arrow keys** - Select option
- **Enter** - Confirm selection
- Options: Low, Medium, High, Critical

**What happens:** Logs priority change to Event Log

#### Notifications Checkbox
- **Space** - Toggle on/off
- **Enter** - Toggle on/off

**What happens:** Logs notification status to Event Log

#### Buttons
- **Tab** to focus button
- **Enter** - Activate button

**Submit:**
- Logs form submission with name
- Increments counter
- Updates Dynamic Table

**Reset:**
- Clears name field
- Logs reset action

### Dynamic Table (Top Right)
**Read-only** - Shows live data:
- **Counter** - Increments on form submit and list select
- **Selected** - Shows last selected list item
- **Timestamp** - Current time (updates live)
- **Mode** - Always "Interactive"

Status column is color-coded:
- 🟢 **Green** - Active/OK/Enabled
- 🔴 **Red** - Inactive/Error/Disabled

### Event Log (Bottom Right)
**Read-only** - Shows timestamped events:
- List selections
- Form submissions
- Priority changes
- Checkbox toggles
- Form resets

**Auto-scrolls** to show latest events.

## Example Usage Flow

1. **Start** - Press **Tab** to focus the Options List (should already be focused)

2. **Select an option:**
   - Press **3** to quick-select "Configure Settings"
   - OR use **↓** arrows and press **Enter**
   - See log entry: `17:58:15 Selected: Configure Settings`
   - See table update: Selected = "Configure Settings", Counter = 1

3. **Move to form:**
   - Press **Tab** to move to Name field
   - Type your name, e.g., "Alice"

4. **Fill form:**
   - Press **Tab** to move to Password
   - Type a password (shows as ***)
   - Press **Tab** to move to Priority dropdown
   - Press **Enter** to open, **↓** to select "High", **Enter** to confirm
   - See log: `17:58:20 Priority changed to: High`

5. **Toggle checkbox:**
   - Press **Tab** to move to checkbox
   - Press **Space** to toggle
   - See log: `17:58:22 Notifications: false` or `true`

6. **Submit form:**
   - Press **Tab** to focus Submit button
   - Press **Enter**
   - See log: `17:58:25 Form submitted! Name: Alice`
   - See table: Counter = 2

7. **Reset form:**
   - Press **Tab** to focus Reset button
   - Press **Enter**
   - Name field clears
   - See log: `17:58:30 Form reset`

8. **Navigate panes:**
   - Press **Ctrl+A** then **|** to split vertically
   - New pane appears with another demo instance
   - Press **Shift+Right** to move between panes

## Testing All Features

### List Navigation
- ✅ Arrow keys work
- ✅ Number shortcuts (1-8) work
- ✅ Enter selects item
- ✅ Selection logs to Event Log
- ✅ Table updates with selected item

### Form Input
- ✅ Text input works (name field)
- ✅ Password masking works
- ✅ Dropdown opens and selects
- ✅ Checkbox toggles
- ✅ Submit button triggers action
- ✅ Reset button clears form

### Dynamic Updates
- ✅ Table refreshes on actions
- ✅ Counter increments
- ✅ Timestamp updates
- ✅ Selected item changes

### Event Logging
- ✅ All actions logged
- ✅ Timestamps shown
- ✅ Color-coded messages
- ✅ Auto-scrolls to latest

### Tab Navigation
- ✅ Tab moves between widgets
- ✅ Focus indicators visible
- ✅ All widgets accessible

## Color Legend

- **Yellow** - Titles and highlighted text
- **Green** - Success messages, active states
- **Cyan** - Info messages
- **Gray** - Timestamps, secondary text
- **Red** - Form widget backgrounds
- **Blue** - List widget background
- **Dark Green** - Table widget background
- **Dark Magenta** - Title bar background

## Troubleshooting

### Demo doesn't appear
- Check that server is running: `make server`
- Check that client connected: `make client`
- Look for errors in server terminal

### Keys don't work
- Make sure client window is focused
- Try clicking in the client window
- Check that terminal supports the keys

### Nothing happens when pressing keys
- Focus might be on wrong widget
- Press **Tab** to cycle through widgets
- Try clicking in a different widget area

### Text doesn't appear in input fields
- Make sure that widget is focused (should have highlight)
- Press **Tab** to focus the name field
- Try typing again

## Next Steps

After testing the demo, you can:
1. Add app selection menu to choose different apps
2. Create custom tview overlays for texelterm
3. Build your own interactive tview apps
4. Combine multiple tview widgets in new ways

## Reference

- TView documentation: https://github.com/rivo/tview
- Texelation TView guide: `docs/TVIEW_USAGE_GUIDE.md`
- Overlay pattern guide: `docs/TVIEW_OVERLAY_PATTERN.md`
