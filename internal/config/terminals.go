package config

// GhosttyConfig contains the default Ghostty configuration
const GhosttyConfig = `# Font Configuration
font-family = Fira Code
font-size = 12

# Window Configuration
window-decoration = false
window-padding-x = 12
window-padding-y = 12
background-opacity = 0.90
background-blur-radius = 32

# Cursor Configuration
cursor-style = block
cursor-style-blink = true

# Scrollback
scrollback-limit = 3023

# Terminal features
mouse-hide-while-typing = true
copy-on-select = true
confirm-close-surface = false

# Disable annoying copied to clipboard
app-notifications = no-clipboard-copy,no-config-reload

# Key bindings for common actions
#keybind = ctrl+c=copy_to_clipboard
#keybind = ctrl+v=paste_from_clipboard
keybind = ctrl+shift+n=new_window
keybind = ctrl+t=new_tab
keybind = ctrl+plus=increase_font_size:1
keybind = ctrl+minus=decrease_font_size:1
keybind = ctrl+zero=reset_font_size

# Material 3 UI elements
unfocused-split-opacity = 0.7
unfocused-split-fill = #44464f

# Tab configuration
gtk-titlebar = false

# Shell integration
shell-integration = detect
shell-integration-features = cursor,sudo,title,no-cursor
keybind = shift+enter=text:\n

# Dank color generation
config-file = ./config-dankcolors
`

// KittyConfig contains the default Kitty configuration
const KittyConfig = `# Font Configuration
font_family Fira Code
font_size 12.0

# Window Configuration
window_padding_width 12
background_opacity 0.90
background_blur 32
hide_window_decorations yes

# Cursor Configuration
cursor_shape block
cursor_blink_interval 1

# Scrollback
scrollback_lines 3000

# Terminal features
copy_on_select yes
strip_trailing_spaces smart

# Key bindings for common actions
map ctrl+shift+n new_window
map ctrl+t new_tab
map ctrl+plus change_font_size all +1.0
map ctrl+minus change_font_size all -1.0
map ctrl+0 change_font_size all 0

# Tab configuration
tab_bar_style powerline
tab_bar_align left

# Shell integration
shell_integration enabled

# Dank color generation
include dank-theme.conf
`
