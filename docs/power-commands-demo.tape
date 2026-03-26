# VHS tape for jamfpro-cli power commands demo
# Regenerate: vhs docs/power-commands-demo.tape
# Requires: brew install charmbracelet/tap/vhs
# NOTE: Requires a configured CLI profile with a real Jamf Pro instance

Output docs/power-commands-demo.mp4

Set Shell "bash"
Set FontSize 12
Set Width 1400
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Padding 16

# Setup PATH and alias (hidden)
Hide
Type "export PATH=./bin:/usr/local/bin:/opt/homebrew/bin:/usr/bin:$PATH"
Enter
Sleep 0.5s
Type "alias jpc='jamfpro-cli -q'"
Enter
Sleep 0.5s
Type "clear"
Enter
Sleep 0.5s
Show

# ─── Scene 1: Instance overview ───────────────────────────────
Type "# Get a full dashboard of your Jamf Pro instance"
Enter
Sleep 0.5s
Type "jpc overview | head -45"
Enter
Sleep 6s

Type "clear"
Enter
Sleep 0.5s

# ─── Scene 2: Audit ──────────────────────────────────────────
Type "# Run health checks — find what needs attention"
Enter
Sleep 0.5s
Type "jpc audit -o table"
Enter
Sleep 6s

Type "clear"
Enter
Sleep 0.5s

# ─── Scene 3: Device compliance ──────────────────────────────
Type "# Which devices are stale? Show managed status + OS version"
Enter
Sleep 0.5s
Type "jpc report device-compliance -o table | head -10"
Enter
Sleep 7s

Type "clear"
Enter
Sleep 0.5s

# ─── Scene 4: Software installs ──────────────────────────────
Type "# What software is installed across the fleet?"
Enter
Sleep 0.5s
Type "jpc report software-installs -o table | head -18"
Enter
Sleep 6s

Type "clear"
Enter
Sleep 0.5s

# Final pause
Sleep 2s
