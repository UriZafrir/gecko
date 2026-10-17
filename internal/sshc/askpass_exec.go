package sshc

import "github.com/gecko-term/gecko/internal/execx"

func execAskpass(prog, prompt string) (string, error) {
	out, err := execx.Command(prog, prompt).Output()
	return string(out), err
}
