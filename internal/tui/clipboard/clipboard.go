package clipboard

import (
	"errors"
	"sync"

	xclipboard "golang.design/x/clipboard"
)

var (
	ErrNoImage     = errors.New("clipboard image not found")
	ErrUnavailable = errors.New("clipboard unavailable")

	initOnce sync.Once
	initErr  error
)

// WriteText 把文本写入系统剪贴板。属于 TUI 输入层能力：只有用户明确触发复制
// （选区复制）时才写入，daemon/core 不访问桌面剪贴板。
func WriteText(s string) error {
	initOnce.Do(func() {
		initErr = xclipboard.Init()
	})
	if initErr != nil {
		return errors.Join(ErrUnavailable, initErr)
	}
	xclipboard.Write(xclipboard.FmtText, []byte(s))
	return nil
}

// ReadImage 读取系统剪贴板里的图片数据。该能力属于 TUI 输入层：只有用户触发粘贴意图时才读取，
// 不在 daemon/core 中访问桌面剪贴板，避免后台服务产生隐式隐私副作用。
func ReadImage() ([]byte, error) {
	initOnce.Do(func() {
		initErr = xclipboard.Init()
	})
	if initErr != nil {
		return nil, errors.Join(ErrUnavailable, initErr)
	}
	data := xclipboard.Read(xclipboard.FmtImage)
	if len(data) == 0 {
		return nil, ErrNoImage
	}
	return data, nil
}
