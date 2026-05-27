# vhs tape for wtkill demo GIF
# render with:  vhs scripts/demo.tape

Output docs/demo.gif

Set Theme "Catppuccin Mocha"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1200
Set Height 720
Set Padding 20
Set TypingSpeed 60ms
Set PlaybackSpeed 0.9
Set Shell zsh

Hide
Type "bash scripts/setup-demo.sh /tmp/wtkill-demo"
Enter
Sleep 4s
Type "clear"
Enter
Show

Type "wtkill --path /tmp/wtkill-demo"
Enter
Sleep 3s

# Browse a few rows
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 800ms

# Delete the highlighted worktree
Space
Sleep 1500ms

# Navigate and delete one more
Down
Sleep 500ms
Space
Sleep 1500ms

# Toggle force (footer flashes)
Type "f"
Sleep 1200ms

# Rescan from clean state
Type "r"
Sleep 2500ms

# Quit
Type "q"
Sleep 600ms
