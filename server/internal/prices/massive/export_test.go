package massive

import (
	"context"
	"time"
)

// SetSleep replaces the wait between retries of c, so that tests can
// record the waits without spending them.
func SetSleep(c *Client, sleep func(context.Context, time.Duration) error) {
	c.sleep = sleep
}
