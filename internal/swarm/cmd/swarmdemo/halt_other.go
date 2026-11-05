//go:build !unix

package main

import "errors"

var errNoStop = errors.New("stopping the node needs a unix system")

func stopNode(int) error   { return errNoStop }
func resumeNode(int) error { return errNoStop }
