@echo off
rem Runs restart.sh through Git Bash, so it works from cmd, PowerShell, Explorer
rem or a double-click without typing `bash` first.
rem
rem   restart              release build, detached
rem   restart --dev        console build
rem   restart --test       vet and test first
rem   restart --no-launch  quit and build, leave it stopped

setlocal

set "BASH=%ProgramFiles%\Git\bin\bash.exe"
if not exist "%BASH%" set "BASH=%ProgramFiles(x86)%\Git\bin\bash.exe"
if not exist "%BASH%" set "BASH=%LOCALAPPDATA%\Programs\Git\bin\bash.exe"
if not exist "%BASH%" (
	echo Git Bash not found. Looked in Program Files, Program Files ^(x86^)
	echo and %%LOCALAPPDATA%%\Programs\Git. Install Git for Windows, or run
	echo restart.sh from a Git Bash prompt.
	exit /b 1
)

rem -l loads the profile so `go` is on PATH the same way it is in an interactive
rem Git Bash. cd first, because the script is invoked by a bare name.
"%BASH%" -lc "cd '%~dp0' && ./restart.sh %*"
exit /b %ERRORLEVEL%
