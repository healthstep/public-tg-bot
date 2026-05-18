package obs

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
)

func RecoverAndExit() {
	r := recover()
	if r == nil {
		return
	}
	st := strings.ReplaceAll(string(debug.Stack()), "\n", " | ")
	line := fmt.Sprintf("panic: %v | %s", r, st)
	if L != nil {
		L.Info("fatal_panic", "detail", line)
	} else {
		log.Print(line)
	}
	os.Exit(1)
}
