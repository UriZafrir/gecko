package sshc

import "os/exec"

func execAskpass(prog, prompt string) (string, error) {
	out, err := exec.Command(prog, prompt).Output()
	return string(out), err
}
