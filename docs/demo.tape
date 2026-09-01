# ---
# relationships:
#   references: usage
# ---

Output docs/assets/demo.gif

Require git
Require gitpr
Require vim

Set Shell "bash"
Set FontSize 20
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set Padding 24
Set Margin 12
Set MarginFill "#11111b"
Set BorderRadius 8
Set WindowBar "Colorful"
Set TypingSpeed 35ms

Hide
Type "export GIT_AUTHOR_NAME='Demo User' GIT_AUTHOR_EMAIL='demo@example.invalid'" Enter
Type "export GIT_COMMITTER_NAME='Demo User' GIT_COMMITTER_EMAIL='demo@example.invalid'" Enter
Type "rm -rf /tmp/gitpr-vhs-demo && mkdir -p /tmp/gitpr-vhs-demo/main && cd /tmp/gitpr-vhs-demo/main" Enter
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
