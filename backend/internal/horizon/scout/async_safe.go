package scout

import (
	"fmt"
	"runtime/debug"
	"strings"
)

func runAsyncSafe(task string, fn func()) {
	if fn == nil {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Printf("[ScoutEngine] async panic recovered: task=%s panic=%v stack=%s\n",
					strings.TrimSpace(task), rec, string(debug.Stack()))
			}
		}()
		fn()
	}()
}
