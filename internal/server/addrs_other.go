//go:build !linux

package server

func labeledAddrs() []ifaceAddr { return nil }
