# ---
# relationships:
#   references: usage
# ---

Output docs/user/assets/demo.gif

Require git
Require gitpr
Require vim

Set Shell "bash"
Set FontSize 20
Set Width 1200
Set Height 720
Set Theme { "name": "gitpr", "background": "#0b0b0c", "foreground": "#ededef", "black": "#0b0b0c", "red": "#ff5f5f", "green": "#87af87", "yellow": "#d7af5f", "blue": "#6aa5ff", "magenta": "#8e8e97", "cyan": "#6aa5ff", "white": "#ededef", "brightBlack": "#808080", "brightRed": "#ff5f5f", "brightGreen": "#87af87", "brightYellow": "#d7af5f", "brightBlue": "#6aa5ff", "brightMagenta": "#8e8e97", "brightCyan": "#6aa5ff", "brightWhite": "#ffffff" }
Set Padding 24
Set Margin 12
Set MarginFill "#0b0b0c"
Set BorderRadius 8
Set WindowBar "Colorful"
Set TypingSpeed 35ms

Hide
Type "unset NO_COLOR; export TERM=xterm-256color COLORTERM=truecolor COLORFGBG='15;0'" Enter
Type "export GIT_AUTHOR_NAME='Demo User' GIT_AUTHOR_EMAIL='demo@example.invalid'" Enter
Type "export GIT_COMMITTER_NAME='Demo User' GIT_COMMITTER_EMAIL='demo@example.invalid'" Enter
Type `DEMO_DIR=$PWD/.tmp/gitpr-vhs-demo; rm -rf "$DEMO_DIR"; mkdir -p "$DEMO_DIR/main"; cd "$DEMO_DIR/main"` Enter
Type "git init -q -b main" Enter
Type "echo 'recipe app' > notes.txt && git add notes.txt && git commit -q -m 'Initial notes'" Enter
Type "git worktree add -b feature/add-step ../feature" Enter
Type "cd ../feature" Enter
Show
Ctrl+L

Sleep 500ms
Type "vim notes.txt" Enter
Sleep 1s
Type "Go"
Type "Step: preheat oven"
Sleep 750ms
Escape
Type ":wq" Enter
Sleep 750ms

Type "git add notes.txt" Enter
Type "git commit -m 'Add prep step'" Enter
Sleep 2s

Type "gitpr create --title 'Add prep step' --description 'Document the prep step.'" Enter
Sleep 3s
Type "cd ../main" Enter
Type "gitpr list" Enter
Sleep 3s

Type "gitpr tui" Enter
Sleep 2s
Enter
Sleep 3s
Type "d"
Sleep 2s
Type "j"
Type "R"
Sleep 2s
Escape
Sleep 500ms
Type "q"
Sleep 1s

Type "gitpr show" Enter
Sleep 2s
Escape
Sleep 1s
Type "gitpr show" Enter
Sleep 2s
Enter
Sleep 2s

Type "COLORFGBG=0\;15 gitpr tui" Enter
Sleep 2s
Type "q"
Sleep 1s
Type "NO_COLOR=1 gitpr tui" Enter
Sleep 2s
Type "q"
Sleep 1s

Hide
Type "PRID=$(gitpr list | tail -1 | awk '{print $1}')" Enter
Show
Type "gitpr review $PRID" Enter
Sleep 4s

Type "gitpr comment $PRID --file notes.txt --line-start 2 --text 'Nice addition.'" Enter
Sleep 2s

Hide
Type "SRC=$(gitpr review $PRID | awk -F': ' '/source_head_sha/{print $2; exit}')" Enter
Type "BASE=$(gitpr review $PRID | awk -F': ' '/base_head_sha/{print $2; exit}')" Enter
Show
Type "gitpr approve $PRID --source-head $SRC --base-head $BASE" Enter
Sleep 2s

Type "gitpr merge $PRID" Enter
Sleep 3s

Type "gitpr list --state merged" Enter
Sleep 3s
Type "git log --oneline --decorate --graph -3" Enter
Sleep 5s
