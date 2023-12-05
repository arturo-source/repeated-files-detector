# Detect repeated files in your OS

Easily detect if you have duplicate files between different folders. This program was created using Go concurrency **to practice**, but I have finished it, so it's ready to be used. It's really useful when you suspect that you have duplicate folders in your system.

Build the program with the Go compiler:

```bash
git clone https://github.com/arturo-source/repeated-files-detector.git
cd repeated-files-detector
go build .
```

Then you can use it typing `./repeated-files-detector -help`, you'll get some help like this:

```txt
Usage of ./repeated-files-detector:
  -directory string
     Directory to evaluate (required)
  -output string
     Name of the file to output the results (default output is stdout)
  -repeated int
     Minimum number of repeated files in two different directories (default 1)
  -threads int
     Number of goroutines running at the same time (default 8)
```

If I want to examine my `code` directory, where I store some projects, I should type something like `./repeated-files-detector -directory ~/code -repeated 3`. This command finds all repeated files recursively in the `code` directory, and shows only the directories which have 3 or more files repeated. For each directory-pair I get somethig like this:

```txt
+------------------------------------------------------------------+
| ../rso-plugin/.git/hooks == ../repeated-files-detector/.git/hooks |
+------------------------------------------------------------------+
|    applypatch-msg.sample == applypatch-msg.sample                |
|        commit-msg.sample == commit-msg.sample                    |
|fsmonitor-watchman.sample == fsmonitor-watchman.sample            |
|    pre-applypatch.sample == pre-applypatch.sample                |
|        pre-commit.sample == pre-commit.sample                    |
|       post-update.sample == post-update.sample                   |
|  pre-merge-commit.sample == pre-merge-commit.sample              |
|       pre-receive.sample == pre-receive.sample                   |
|          pre-push.sample == pre-push.sample                      |
|        pre-rebase.sample == pre-rebase.sample                    |
|prepare-commit-msg.sample == prepare-commit-msg.sample            |
|sendemail-validate.sample == sendemail-validate.sample            |
|  push-to-checkout.sample == push-to-checkout.sample              |
|            update.sample == update.sample                        |
+------------------------------------------------------------------+
```
