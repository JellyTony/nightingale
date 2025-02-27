package sender

import (
	"bytes"
	"os"
	"os/exec"
	"time"

	"github.com/ccfos/nightingale/v6/memsto"

	"github.com/toolkits/pkg/file"
	"github.com/toolkits/pkg/logger"
	"github.com/toolkits/pkg/sys"
)

func MayPluginNotify(noticeBytes []byte, notifyScript *memsto.NotifyScriptCacheType) {
	if len(noticeBytes) == 0 {
		return
	}
	alertingCallScript(noticeBytes, notifyScript)
}

func alertingCallScript(stdinBytes []byte, notifyScript *memsto.NotifyScriptCacheType) {
	// not enable or no notify.py? do nothing
	config := notifyScript.GetNotifyScript()
	if !config.Enable || config.Content == "" {
		return
	}

	fpath := ".notify_scriptt"
	if config.Type == 1 {
		fpath = config.Content
	} else {
		rewrite := true
		if file.IsExist(fpath) {
			oldContent, err := file.ToString(fpath)
			if err != nil {
				logger.Errorf("event_notify: read script file err: %v", err)
				return
			}

			if oldContent == config.Content {
				rewrite = false
			}
		}

		if rewrite {
			_, err := file.WriteString(fpath, config.Content)
			if err != nil {
				logger.Errorf("event_notify: write script file err: %v", err)
				return
			}

			err = os.Chmod(fpath, 0777)
			if err != nil {
				logger.Errorf("event_notify: chmod script file err: %v", err)
				return
			}
		}
		fpath = "./" + fpath
	}

	cmd := exec.Command(fpath)
	cmd.Stdin = bytes.NewReader(stdinBytes)

	// combine stdout and stderr
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := startCmd(cmd)
	if err != nil {
		logger.Errorf("event_notify: run cmd err: %v", err)
		return
	}

	err, isTimeout := sys.WrapTimeout(cmd, time.Duration(config.Timeout)*time.Second)

	if isTimeout {
		if err == nil {
			logger.Errorf("event_notify: timeout and killed process %s", fpath)
		}

		if err != nil {
			logger.Errorf("event_notify: kill process %s occur error %v", fpath, err)
		}

		return
	}

	if err != nil {
		logger.Errorf("event_notify: exec script %s occur error: %v, output: %s", fpath, err, buf.String())
		return
	}

	logger.Infof("event_notify: exec %s output: %s", fpath, buf.String())
}
