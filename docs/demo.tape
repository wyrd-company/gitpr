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
Type "rm -rf /tmp/gitpr-vhs-demo && mkdir -p /tmp/gitpr-vhs-demo && cd /tmp/gitpr-vhs-demo" Enter
Show
Ctrl+L

Sleep 500ms
Type "git clone --depth 1 https://github.com/octocat/Hello-World.git hello-world" Enter
Sleep 3s
Type "cd hello-world" Enter
Type "git worktree add -b demo/greeting ../hello-world-greeting" Enter
Sleep 2s
Type "cd ../hello-world-greeting" Enter

Type "vim greeting.txt" Enter
Sleep 1s
Type "i"
Type "Hello from a Git worktree."
Sleep 750ms
Escape
Type ":wq" Enter
Sleep 750ms

Type "git status --short" Enter
Sleep 1s
Type "git add greeting.txt" Enter
Type "git commit -m 'Add greeting'" Enter
Sleep 2s

Type "gitpr create --title 'Add greeting' --description 'Add a short greeting example.'" Enter
Sleep 3s
Type "cd ../hello-world" Enter
Type "gitpr list" Enter
Sleep 3s

Type "gitpr tui" Enter
Sleep 2s
Enter
Sleep 3s
Type "jj"
Sleep 1s
Type "c"
Sleep 750ms
Type "Clear and concise."
Sleep 1s
Ctrl+S
Sleep 2s
Type "o"
Sleep 3s
Type "m"
Sleep 1s
Type "y"
Sleep 1s
Type "y"
Sleep 4s
Type "q"
Sleep 1s

Type "gitpr list --status approved" Enter
Sleep 3s
Type "git worktree list" Enter
Sleep 2s
Type "git log --oneline --decorate --graph -3" Enter
Sleep 5s
