package texeluicli

import (
	"fmt"
	"time"
)

func newSessionID() string {
	return fmt.Sprintf("sess-%d", time.Now().UnixNano())
}
