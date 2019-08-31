package main

import "os/exec"

func runCmd(name, dir string, arg ...string) (out string, err error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir

	bs, err := cmd.CombinedOutput()
	if err != nil {
		return
	}
	return string(bs), err
}
