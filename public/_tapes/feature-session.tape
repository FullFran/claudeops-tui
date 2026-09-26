# Session drill-down detail. Run from repo root (see tabs.tape header).
# NOTE: the browse->detail transition leaves two ttyd viewport-diff ghost bands
# (a stray breadcrumb + the browse table header). generate.sh crops them out.
# If the layout/font changes, re-check the crop offsets in generate.sh.
# vhs public/_tapes/feature-session.tape
Output "public/_tapes/.render.gif"
Set Shell "bash"
Set FontSize 16
Set Width 1300
Set Height 1120
Set Padding 18

Type "./claudeops"
Enter
Sleep 6s
Type "2"
Sleep 1500ms
Enter
Sleep 1500ms
Enter
Sleep 3500ms
Screenshot public/13-session-detail.png
