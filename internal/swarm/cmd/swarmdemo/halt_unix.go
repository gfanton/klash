//go:build unix

package main

import "syscall"

// stopNode freezes the chain's node (SIGSTOP): no blocks, no answers.
func stopNode(pid int) error { return syscall.Kill(pid, syscall.SIGSTOP) }

// resumeNode lets a frozen node go on (SIGCONT).
func resumeNode(pid int) error { return syscall.Kill(pid, syscall.SIGCONT) }
