# Help overlay + Dashboard day drill-down. Run from repo root (see tabs.tape header).
# vhs public/_tapes/feature-help-day.tape
Output "public/_tapes/.render.gif"
Set Shell "bash"
Set FontSize 16
Set Width 1300
Set Height 860
Set Padding 18

Type "./claudeops"
Enter
Sleep 6s
Type "?"
Sleep 1s
Screenshot public/09-help.png
Escape
Sleep 800ms
Enter
Sleep 1500ms
Screenshot public/10-day-browse.png
Enter
Sleep 3s
Screenshot public/11-day-detail.png
