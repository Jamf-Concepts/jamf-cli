# VHS tape for jamfpro-cli README demo GIF
# Regenerate: vhs docs/demo.tape
# Requires: brew install charmbracelet/tap/vhs

Output docs/demo.gif

Set Shell "bash"
Set FontSize 11
Set Width 1300
Set Height 350
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms
Set Padding 16

# Setup PATH (hidden)
Hide
Type "export PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:$PATH"
Enter
Sleep 0.5s
Type "clear"
Enter
Sleep 0.5s
Show

# Scene 1: Version
Type "jamfpro-cli version"
Enter
Sleep 1.5s

# Scene 2: List computers (limited to 3)
Type "jamfpro-cli comp list --limit 3 -o table"
Enter
Sleep 4s

# Final pause
Sleep 1s
