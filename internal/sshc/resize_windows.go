package sshc

// watchResize: Windows has no SIGWINCH; the size is set once.
func watchResize(f func()) (stop func()) { return func() {} }
