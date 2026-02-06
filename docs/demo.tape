# VHS tape for jamfpro-cli README demo GIF
# Regenerate: vhs docs/demo.tape
# Requires: brew install charmbracelet/tap/vhs

Output docs/demo.gif

Set Shell "bash"
Set FontSize 13
Set Width 1100
Set Height 550
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

# Scene 2: Command discovery — table format
Type "jamfpro-cli commands -o table | head -15"
Enter
Sleep 3s

# Scene 3: Command discovery — JSON piped to jq
Type "jamfpro-cli commands -o json | jq '.[0:3]'"
Enter
Sleep 3s

# Final pause
Sleep 1s
