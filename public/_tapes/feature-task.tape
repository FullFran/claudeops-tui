# New-task input modal. Run from repo root (see tabs.tape header).
# vhs public/_tapes/feature-task.tape
Output "public/_tapes/.render.gif"
Set Shell "bash"
Set FontSize 16
Set Width 1300
Set Height 860
Set Padding 18

Type "./claudeops"
Enter
Sleep 6s
Type "n"
Sleep 800ms
Type "refactor auth module"
Sleep 700ms
Screenshot public/12-task-input.png
