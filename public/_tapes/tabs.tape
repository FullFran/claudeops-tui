# All 8 tabs. Run from repo root after: CGO_ENABLED=0 go build -o claudeops ./cmd/claudeops
# vhs public/_tapes/tabs.tape
Output "public/_tapes/.render.gif"
Set Shell "bash"
Set FontSize 16
Set Width 1300
Set Height 860
Set Padding 18

Type "./claudeops"
Enter
Sleep 6s
Screenshot public/01-dashboard.png
Type "2"
Sleep 1500ms
Screenshot public/02-sessions.png
Type "3"
Sleep 1500ms
Screenshot public/03-projects.png
Type "4"
Sleep 1500ms
Screenshot public/04-models.png
Type "5"
Sleep 1500ms
Screenshot public/05-tasks.png
Type "6"
Sleep 1500ms
Screenshot public/06-insights.png
Type "7"
Sleep 1500ms
Screenshot public/07-classroom.png
Type "8"
Sleep 1500ms
Screenshot public/08-settings.png
